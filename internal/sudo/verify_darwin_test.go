package sudo

import (
	"errors"
	"os/user"
	"testing"
)

// TestCheckPasswordDarwin runs CheckPassword against a fake dscl that also
// checks it gets `. -authonly <user>` and no password argument.
func TestCheckPasswordDarwin(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Skipf("no current user: %v", err)
	}
	fake := writeScript(t, `[ "$*" = `+shQuote(". -authonly "+u.Username)+` ] || exit 2
stty -echo
printf 'Password:'
IFS= read -r line
stty echo
[ "$line" = 'secret' ]
`)
	old := dsclPath
	dsclPath = fake
	t.Cleanup(func() { dsclPath = old })

	if err := CheckPassword("/usr/bin/sudo", "", nil, []byte("secret")); err != nil {
		t.Fatalf("right password: %v", err)
	}
	if err := CheckPassword("/usr/bin/sudo", "", nil, []byte("nope")); !errors.Is(err, errIncorrect) {
		t.Fatalf("wrong password: err = %v, want errIncorrect", err)
	}
}
