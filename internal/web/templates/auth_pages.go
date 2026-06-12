package templates

import (
	"context"
	"fmt"
	"html"
	"io"

	"github.com/a-h/templ"
)

func LoginPage(flash string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		flashHTML := ""
		if flash != "" {
			flashHTML = `<p class="auth-err">` + html.EscapeString(flash) + `</p>`
		}
		_, err := fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Sign in — WhatsApp Tool</title>
<link rel="stylesheet" href="/static/tokens.css">
<link rel="stylesheet" href="/static/app.css">
</head>
<body class="auth-page">
<div class="auth-card">
<h1 class="auth-title">Sign in</h1>
%s
<form method="post" action="/login" class="auth-form">
<label class="field"><span>Email</span>
<input type="email" name="email" required autofocus autocomplete="email"></label>
<label class="field"><span>Password</span>
<input type="password" name="password" required autocomplete="current-password"></label>
<button class="btn pri" type="submit">Sign in</button>
</form>
</div>
</body></html>`, flashHTML)
		return err
	})
}

func InvitePage(name, token, errMsg string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		errHTML := ""
		if errMsg != "" {
			errHTML = `<p class="auth-err">` + html.EscapeString(errMsg) + `</p>`
		}
		_, err := fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Accept invite — WhatsApp Tool</title>
<link rel="stylesheet" href="/static/tokens.css">
<link rel="stylesheet" href="/static/app.css">
</head>
<body class="auth-page">
<div class="auth-card">
<h1 class="auth-title">Welcome, %s</h1>
<p>Set your password to activate your account.</p>
%s
<form method="post" action="/invite/%s" class="auth-form">
<label class="field"><span>Password</span>
<input type="password" name="password" required minlength="8" autocomplete="new-password"></label>
<label class="field"><span>Confirm password</span>
<input type="password" name="confirm" required minlength="8" autocomplete="new-password"></label>
<button class="btn pri" type="submit">Activate account</button>
</form>
</div>
</body></html>`, html.EscapeString(name), errHTML, html.EscapeString(token))
		return err
	})
}

func OnboardingPage(flash string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		flashHTML := ""
		if flash != "" {
			flashHTML = `<p class="auth-err">` + html.EscapeString(flash) + `</p>`
		}
		_, err := fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Setup — WhatsApp Tool</title>
<link rel="stylesheet" href="/static/tokens.css">
<link rel="stylesheet" href="/static/app.css">
</head>
<body class="auth-page">
<div class="auth-card">
<h1 class="auth-title">First-time setup</h1>
<p>Create your admin account to get started.</p>
%s
<form method="post" action="/onboarding/admin" class="auth-form">
<label class="field"><span>Name</span>
<input type="text" name="name" required autofocus></label>
<label class="field"><span>Email</span>
<input type="email" name="email" required autocomplete="email"></label>
<label class="field"><span>Password</span>
<input type="password" name="password" required minlength="8"></label>
<label class="field"><span>Confirm password</span>
<input type="password" name="confirm" required minlength="8"></label>
<button class="btn pri" type="submit">Create admin account</button>
</form>
</div>
</body></html>`, flashHTML)
		return err
	})
}
