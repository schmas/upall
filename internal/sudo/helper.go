package sudo

import (
	"io"
	"net"
	"time"
)

// helperTimeout bounds the connect and the read of the helper.
const helperTimeout = 3 * time.Second

// maxReply caps the reply size the helper accepts.
const maxReply = 1 << 16

// RunHelper connects to sock, copies the reply to out and adds "\n".
// It returns 0 on a non-empty reply, else 1. On failure it writes nothing to
// out, because sudo treats any output as the password.
// Called as: upall __sudo-askpass <sock>.
func RunHelper(sock string, out io.Writer) int {
	conn, err := net.DialTimeout("unix", sock, helperTimeout)
	if err != nil {
		return 1
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(helperTimeout))

	reply, err := io.ReadAll(io.LimitReader(conn, maxReply))
	defer zero(reply)
	if err != nil || len(reply) == 0 {
		return 1
	}
	reply = append(reply, '\n')
	if _, err := out.Write(reply); err != nil {
		return 1
	}
	return 0
}
