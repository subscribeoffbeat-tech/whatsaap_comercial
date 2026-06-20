package templates

import (
	"context"
	"fmt"
	"html"
	"io"

	"github.com/a-h/templ"
)

// ErrorPage renders a branded error page inside the app shell.
// Pass nil for agent on unauthenticated error paths — shell handles it gracefully.
func ErrorPage(code int, title, detail string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, ShellOpen(nil, "", title, "")); err != nil {
			return err
		}
		_, err := fmt.Fprintf(w,
			`<div class="page-wrap" style="min-height:60vh;display:flex;align-items:center;justify-content:center;text-align:center">
<div>
  <div style="font-size:72px;font-weight:800;color:var(--accent);line-height:1;margin-bottom:12px">%d</div>
  <h1 style="font-size:22px;font-weight:700;color:var(--text-strong);margin:0 0 8px">%s</h1>
  <p style="font-size:14px;color:var(--text-secondary);margin:0 0 24px;max-width:380px">%s</p>
  <a class="btn btn-primary" href="/">Go to dashboard</a>
</div>
</div>`,
			code,
			html.EscapeString(title),
			html.EscapeString(detail),
		)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, ShellClose())
		return err
	})
}
