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
<title>Sign in — Offbeat ChatFlow</title>
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
<button class="btn btn-primary" type="submit">Sign in</button>
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
<title>Accept invite — Offbeat ChatFlow</title>
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
<button class="btn btn-primary" type="submit">Activate account</button>
</form>
</div>
</body></html>`, html.EscapeString(name), errHTML, html.EscapeString(token))
		return err
	})
}

// OnboardingStatus holds setup checklist state for the WhatsApp connection step.
type OnboardingStatus struct {
	WaConnected       bool // PhoneNumberID + AccessToken both set in env
	PinSet            bool // wa_pin_set in app_config
	DisplayNameOK     bool // display_name_approved in app_config
	BusinessProfileOK bool // business_profile_complete in app_config
	BusinessVerified  bool // business_verified in app_config
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
<title>Setup — Offbeat ChatFlow</title>
<link rel="stylesheet" href="/static/tokens.css">
<link rel="stylesheet" href="/static/app.css">
</head>
<body class="auth-page">
<div class="auth-card">
<h1 class="auth-title">First-time setup</h1>
<ol class="onb-steps">
<li class="s on">Create admin account</li>
<li class="s">Connect WhatsApp</li>
<li class="s">Configure settings</li>
<li class="s">Ready</li>
</ol>
<p style="color:var(--text-muted);font-size:0.875rem;margin-bottom:1rem">Create your admin account to get started.</p>
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
<button class="btn btn-primary" type="submit">Create admin account</button>
</form>
</div>
</body></html>`, flashHTML)
		return err
	})
}

func OnboardingSetupPage(status OnboardingStatus) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Setup — Offbeat ChatFlow</title>
<link rel="stylesheet" href="/static/tokens.css">
<link rel="stylesheet" href="/static/app.css">
</head>
<body class="auth-page">
<div class="auth-card" style="max-width:480px">
<h1 class="auth-title">Connect WhatsApp</h1>
<ol class="onb-steps">
<li class="s done">Create admin account</li>
<li class="s on">Connect WhatsApp</li>
<li class="s">Configure settings</li>
<li class="s">Ready</li>
</ol>
<p style="color:var(--text-muted);font-size:0.875rem;margin-bottom:1.25rem">
WhatsApp credentials are loaded from environment variables. Check each item below and
configure missing values on your server before proceeding.
</p>
<div class="onb-checklist">
%s%s%s%s%s</div>
<div style="display:flex;justify-content:flex-end;margin-top:1.5rem">
<a class="btn btn-primary" href="/">Continue to app →</a>
</div>
</div>
</body></html>`,
			ciItem(status.WaConnected, "Connect number &amp; token",
				"Set WA_PHONE_NUMBER_ID, WA_WABA_ID, and WA_ACCESS_TOKEN in your environment"),
			ciItem(status.PinSet, "Set 6-digit PIN",
				"Set the WhatsApp Business account PIN via Meta Business Manager"),
			ciItem(status.DisplayNameOK, "Display-name approved",
				"Awaiting Meta approval for your WhatsApp display name"),
			ciItem(status.BusinessProfileOK, "Business profile complete",
				"Fill in your business profile in Meta Business Manager"),
			ciItem(status.BusinessVerified, "Business verification",
				"Complete business verification in Meta Business Manager"),
		)
		return err
	})
}

func ciItem(ok bool, label, hint string) string {
	class := "ci no"
	icon := `<span class="ci-icon">–</span>`
	if ok {
		class = "ci ok"
		icon = `<span class="ci-icon">✓</span>`
	}
	s := fmt.Sprintf(`<div class="%s">%s<div><div class="ci-lbl">%s</div>`, class, icon, label)
	if !ok {
		s += fmt.Sprintf(`<div class="ci-hint">%s</div>`, hint)
	}
	s += `</div></div>`
	return s
}
