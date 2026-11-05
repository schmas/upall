package sudo

// CheckPassword tests pw without accepting a biometric approval where possible.
// linux: srv.Arm(pw, 1), then `<realSudo> -k -A true` with SUDO_ASKPASS=askpass,
// output discarded. If a biometric PAM module (pam_fprintd, pam_u2f) approves,
// the typed password is not proven; this is an accepted limitation.
func CheckPassword(realSudo, askpass string, srv *Server, pw []byte) error {
	return checkWithAskpass(realSudo, askpass, srv, pw)
}
