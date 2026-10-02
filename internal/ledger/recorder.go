package ledger

import (
	"sync"
	"sync/atomic"
	"time"
)

// Recorder is how a DECISION PATH reaches the log, and its whole design is one
// sentence from the package comment made structural: the ledger is memory,
// never truth, so a ledger write must not be able to fail a decision.
//
// TWO THINGS MAKE THAT STRUCTURAL RATHER THAN A CONVENTION EVERY CALL SITE HAS
// TO REMEMBER.
//
//   - RECORD RETURNS NOTHING. `Log` and `Append` return errors, and an error
//     return is an invitation: sooner or later a call site writes
//     `if err := ledger.Log(...); err != nil { return err }` and a read-only
//     filesystem starts refusing accepts. There is no error value here to
//     propagate, so no caller can propagate one. The failures are counted and
//     readable (Failed, Dropped) for anyone who wants to know; they are not
//     returned to the reviewer.
//   - THE APPEND HAPPENS ON ANOTHER GOROUTINE. Nothing the filesystem does —
//     a slow disk, a network mount, a directory that will not create — can
//     land inside the request that made the decision. A panic in that
//     goroutine would still take the process down, so the worker recovers:
//     the point is that no failure mode here reaches the decision, and
//     "except panics" is not that.
//
// THE COST, STATED. The queue is bounded and a full queue DROPS the record
// rather than blocking the decision (see Record), and a process that exits
// without calling Flush loses whatever is still queued. Both are losses of
// MEMORY, which is what this store holds; neither can lose a decision, which
// lives in the .md and its versions, and neither can corrupt the log, because
// every record still goes out through Append's single atomic write.
type Recorder struct {
	// log is Log in production and the test seam everywhere else — the only
	// way to make a ledger write FAIL on demand, which is what the
	// never-fails-a-decision property has to be proven against.
	log func(docPath string, rec Record) error

	// ch is built in NewRecorder and never replaced, so no reader of it needs
	// synchronisation. Only the WORKER is lazy (start), because a recorder
	// nobody records through must not cost a goroutine — DefaultRecorder exists
	// in every galley process, including the ones that never decide anything.
	ch    chan job
	start sync.Once
	// live says the worker is running, so Flush knows whether there is anything
	// to drain and Close knows whether anyone will ever read the channel again.
	live atomic.Bool

	// closed and stop are how the worker is shut down, and THERE IS NO MUTEX
	// HERE ON PURPOSE.
	//
	// There was one: an RWMutex that Record took for reading and Close took for
	// writing, with the channel send inside it so a send could never race the
	// close that would make it panic. It made Record's "never blocks" FALSE in
	// exactly the shape the type forbids. Flush held the read lock across a
	// BLOCKING send, Close wanted the write lock, and Go's RWMutex blocks new
	// readers behind a pending writer — so a Close concurrent with a wedged
	// append put every deciding goroutine to sleep on the disk, which is the one
	// thing this type exists to prevent. Unreachable today (DefaultRecorder is
	// never Closed and Close has no caller outside tests), and a claim a
	// comment made unconditionally is not a claim to leave conditionally true.
	//
	// So the channel is NEVER CLOSED and the worker is told to stop through a
	// second one. A send on a channel that is never closed cannot panic, so
	// Record needs no lock at all: it reads an atomic and does a non-blocking
	// send, both wait-free. What that costs is stated in Close.
	closed   atomic.Bool
	stop     chan struct{}
	stopOnce sync.Once

	failed  atomic.Int64
	dropped atomic.Int64
}

// job is one record to append, or — when done is non-nil — the FLUSH SENTINEL:
// a marker that carries no record and is answered by closing done.
//
// THE SENTINEL IS WHY THERE IS NO sync.WaitGroup HERE ANY MORE. Waiting on one
// meant `wg.Add(1)` on the caller's goroutine, which panics with "WaitGroup
// misuse: Add called concurrently with Wait" if a Record lands in the window
// where a non-empty queue has just drained to zero with a Flush blocked on it —
// the one panic path the worker's recover cannot see, on the goroutine that is
// making the decision. The channel already orders everything: one worker reads
// it in FIFO, so when the sentinel comes back every record queued before it has
// been written. That is the same guarantee with no counter to misuse.
type job struct {
	doc  string
	rec  Record
	done chan struct{}
}

// queueDepth is how many records may be in flight before Record starts
// dropping. Generous next to any real burst — the largest is a bulk sweep,
// which is one record per pending proposal — and small enough that a wedged
// filesystem cannot grow the queue without bound.
const queueDepth = 256

// NewRecorder builds a recorder over a custom append. Production uses
// DefaultRecorder, whose append is Log; tests use this to make the append fail.
func NewRecorder(log func(docPath string, rec Record) error) *Recorder {
	return &Recorder{log: log, ch: make(chan job, queueDepth), stop: make(chan struct{})}
}

// DefaultRecorder is the process-wide recorder every decision path writes
// through. One per process, so one goroutine appends and the log's line order
// is the order decisions were made.
//
// It is deliberately never Closed: it lives as long as the process, and the
// last thing that happens to it is a Flush on the way out (see Flush below).
// Close is for a recorder with a shorter life than its process — which in
// practice means a test's.
var DefaultRecorder = NewRecorder(Log)

// Record remembers one decision about docPath. It never fails, NEVER BLOCKS —
// on the filesystem, on a lock, or on another goroutine — and never returns
// anything a caller could mistake for a reason to refuse the decision.
//
// "Never blocks" is meant literally and is why there is no mutex in this type:
// an atomic load and a non-blocking send is the whole body. See the `closed`
// field for the version of this that was only nearly true.
//
// A FULL QUEUE DROPS, and that is the right way round. Blocking would put the
// decision behind the disk, which is the one thing this type exists to prevent;
// the dropped record is a memory, and the decision it was about has already
// landed in the document.
//
// THE INSTANT IS STAMPED HERE, WHICH IS DECISION TIME. Append fills a zero At
// as a backstop, but Append runs on the worker, so under any queue at all that
// backstop records when the WRITE happened rather than when the decision was
// made. It showed: the trust verdict enqueues one record per swept proposal and
// then the verdict itself, and with only the verdict carrying its own stamp the
// verdict's `at` was strictly EARLIER than the proposals it had just swept.
// Index.Decisions orders by at_unix, so the log read itself back in an order
// that never happened. Spec decision 6 — "recorded at decision time, not swept
// up at the end" — is about this instant as much as about the call site.
func (r *Recorder) Record(docPath string, rec Record) {
	if r == nil {
		return
	}
	if rec.At.IsZero() {
		rec.At = time.Now().UTC()
	}
	if r.closed.Load() {
		r.dropped.Add(1)
		return
	}
	r.start.Do(func() {
		r.live.Store(true)
		go r.work()
	})
	select {
	case r.ch <- job{doc: docPath, rec: rec}:
	default:
		r.dropped.Add(1)
	}
}

// Flush waits for every record queued WHEN IT WAS CALLED to be written, up to
// timeout, and reports whether the queue drained. A caller about to exit calls
// it; nobody has to.
//
// It puts a sentinel through the same channel and waits for the worker to reach
// it. FIFO plus one worker is the whole proof: everything ahead of the sentinel
// has been written by the time the sentinel is answered. A record enqueued
// after the call is not waited for, which is the honest reading of "flush what
// is queued" and the only one a concurrent decision path can support.
func (r *Recorder) Flush(timeout time.Duration) bool {
	if r == nil {
		return true
	}
	deadline := time.After(timeout)
	done := make(chan struct{})

	if r.closed.Load() || !r.live.Load() {
		// Nothing was ever recorded through this recorder (or it is shut), so
		// there is nothing queued and nobody to answer a sentinel.
		return true
	}
	// A BLOCKING send, bounded by the deadline: the sentinel must not be
	// dropped the way a record is, or Flush would report a drain it never
	// waited for. IT TAKES NO LOCK. It used to hold a read lock across this
	// very send so Close could not land in the middle, which is precisely how a
	// wedged append reached Record — see the `closed` field. The channel is
	// never closed, so there is nothing here to be protected from; a Flush
	// racing a Close waits for the worker or for the deadline, and `stop` ends
	// the wait either way.
	select {
	case r.ch <- job{done: done}:
	case <-r.stop:
		return true
	case <-deadline:
		return false
	}

	select {
	case <-done:
		return true
	case <-deadline:
		return false
	}
}

// Close flushes and then stops the worker, and it exists because that worker
// would otherwise outlive every recorder ever built, for the life of the
// process. Harmless for DefaultRecorder, which IS the process; a leak per test
// otherwise, which is where its only callers are.
//
// It reports what Flush reported. After it, Record drops: `closed` is set
// before the worker is told to stop, so a decision path that reads it sees the
// truth without waiting for anything.
//
// IT CLOSES `stop`, NEVER `ch`, and that asymmetry is the whole design. Closing
// the queue would mean a send could panic, which would mean guarding the send,
// which is the lock Record is not allowed to take. THE COST, STATED: a Record
// that passes the `closed` check in the instant before Close lands may leave
// its job in the buffer after the worker's final drain, unwritten and NOT
// counted in Dropped. That is a memory lost in a race with a shutdown, on a
// path that has no caller outside tests — against a decision-time wait on a
// wedged disk, which is the harm this type was built to make impossible.
func (r *Recorder) Close(timeout time.Duration) bool {
	if r == nil {
		return true
	}
	ok := r.Flush(timeout)
	r.closed.Store(true)
	r.stopOnce.Do(func() { close(r.stop) })
	return ok
}

// Failed is how many records the append refused. Dropped is how many never
// reached it because the queue was full (or the recorder was closed). Both
// exist so a test can prove the write really was attempted and really did fail
// — a fail-soft path that silently did nothing would pass a naive version of
// that test — and so a CLI can say on the way out that it lost something.
func (r *Recorder) Failed() int  { return int(r.failed.Load()) }
func (r *Recorder) Dropped() int { return int(r.dropped.Load()) }

// work reads the queue until Close says stop, and then DRAINS WHAT IS LEFT
// before returning — which is what the old `range r.ch` gave for free and what
// a bare `return` on stop would have quietly taken away. Everything already
// queued when the stop lands is still written, sentinels included, so a Flush
// racing a Close is answered rather than left hanging.
func (r *Recorder) work() {
	for {
		select {
		case <-r.stop:
			r.drain()
			return
		case j := <-r.ch:
			r.handle(j)
		}
	}
}

func (r *Recorder) drain() {
	for {
		select {
		case j := <-r.ch:
			r.handle(j)
		default:
			return
		}
	}
}

func (r *Recorder) handle(j job) {
	if j.done != nil {
		close(j.done)
		return
	}
	r.one(j)
}

// one writes a single record, swallowing everything.
//
// The recover is not defensive padding: this goroutine belongs to no request,
// so a panic here would take the whole process down and end the very review the
// record was about — which is the same harm as failing the decision, arriving a
// moment later.
func (r *Recorder) one(j job) {
	defer func() {
		if v := recover(); v != nil {
			r.failed.Add(1)
		}
	}()
	if err := r.log(j.doc, j.rec); err != nil {
		r.failed.Add(1)
	}
}

// There is deliberately NO package-level `Record` helper beside Flush: the
// record TYPE already owns that name, and a `ledger.Record(...)` call reading
// like a constructor for a `ledger.Record` value is a confusion nobody needs in
// a decision path. Call sites say `ledger.DefaultRecorder.Record(...)`, which
// also names the thing a test can substitute.

// Flush drains the default recorder. There is exactly ONE caller — cmd/galley's
// main, on the way out — and that is worth stating because the comment here
// used to claim a second one at the edit server's shutdown: shutdownEdit's
// "final flush" is the PROJECTION's, which puts the document on disk, and it
// says nothing about the ledger. A caller that forgets loses queued memories
// and nothing else; `galley edit` exits through main like every other command,
// so the one flush covers it.
func Flush(timeout time.Duration) bool { return DefaultRecorder.Flush(timeout) }
