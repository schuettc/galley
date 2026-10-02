package unsent

import (
	"strings"
	"testing"
	"time"

	"github.com/schuettc/galley/internal/ondisk"
	"github.com/schuettc/galley/internal/review"
)

// THE KEY SET IS THE CONTRACT. The decoder is strict, so a rename is refused
// on read — but only by the NEXT build, against a file this one wrote. This
// list goes red on the commit that renames. Every field is populated, so a new
// key turns it red too: adding one is a decision about whether Version moves.
func TestTheUnsentKeysAreTheContract(t *testing.T) {
	for _, tc := range []struct {
		name string
		val  any
		want string
	}{
		{"File", File{V: 1, Comments: []Comment{{Key: "k"}}}, "v,comments"},
		{"Comment", Comment{
			Key: "cb-0123456789abcdef", Kind: KindBlock, Text: "t", Author: review.AuthorCourt, At: time.Now(),
			BlockKind: "image", Region: &review.Region{X: 0, Y: 0, W: 1, H: 1}, Quote: "q",
		}, "key,kind,text,author,at,blockKind,region,quote"},
	} {
		keys, err := ondisk.Keys(tc.val)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(keys, ","); got != tc.want {
			t.Errorf("%s keys = %s\n            want %s\n"+
				"a renamed key is refused by the next build; a new key needs a Version decision", tc.name, got, tc.want)
		}
	}
}
