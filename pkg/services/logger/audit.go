package logger

import "context"

// Audit records a security operation's outcome using the request's correlation
// fields. Event and outcome must be static labels, never credentials, addresses,
// token values, or provider errors. Emit success only after the operation commits.
func Audit(ctx context.Context, event, outcome string) {
	if l := Get(ctx); l != nil {
		l.InfoContext(ctx, "Security event", "event", event, "outcome", outcome)
	}
}
