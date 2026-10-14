package email

import (
	"fmt"
	"html"
	"net/url"
	"strings"
	"time"
)

// defaultBrand names the product when the public URL is unset.
const defaultBrand = "go-core-kit"

// BrandName returns the public host as the brand, mirroring the reference.
func BrandName(publicAppURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(publicAppURL))
	if err != nil || parsed.Hostname() == "" {
		return defaultBrand
	}
	return parsed.Hostname()
}

// VerifyEmailURL joins the verification path with the token; an unparsable
// public URL yields an empty string, mirroring the reference.
func VerifyEmailURL(publicAppURL, token string) string {
	return joinURL(publicAppURL, "/verify-email", token)
}

// ResetPasswordURL joins the reset path with the token.
func ResetPasswordURL(publicAppURL, token string) string {
	return joinURL(publicAppURL, "/reset-password", token)
}

// VerificationEmail renders the verification message the reference sends.
func VerificationEmail(to, brand, token, verifyURL string, expiresAt time.Time) Message {
	expiresAtIso := expiresAt.UTC().Format(time.RFC3339)
	lines := []string{"Use this token to verify your email:", "", token, ""}
	if verifyURL != "" {
		lines = append(lines, "Or open this link:", verifyURL, "")
	}
	lines = append(lines, "This token expires at "+expiresAtIso+".")
	return Message{
		To:      to,
		Subject: "Verify your email",
		Text:    strings.Join(lines, "\n"),
		HTML:    renderHTML(brand, "Verify your email", token, verifyURL, expiresAtIso),
	}
}

// PasswordResetEmail renders the password reset message.
func PasswordResetEmail(to, brand, token, resetURL string, expiresAt time.Time) Message {
	expiresAtIso := expiresAt.UTC().Format(time.RFC3339)
	lines := []string{"Use this token to reset your password:", "", token, ""}
	if resetURL != "" {
		lines = append(lines, "Or open this link:", resetURL, "")
	}
	lines = append(lines, "This token expires at "+expiresAtIso+".")
	return Message{
		To:      to,
		Subject: "Reset your password",
		Text:    strings.Join(lines, "\n"),
		HTML:    renderHTML(brand, "Reset your password", token, resetURL, expiresAtIso),
	}
}

// joinURL replaces the base path with the given path and sets the token
// query, mirroring the reference's `new URL(path, publicAppUrl)`.
func joinURL(publicAppURL, path, token string) string {
	parsed, err := url.Parse(strings.TrimSpace(publicAppURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.Path = path
	parsed.RawPath = ""
	parsed.RawQuery = url.Values{"token": {token}}.Encode()
	parsed.Fragment = ""
	return parsed.String()
}

// renderHTML renders the shared branded shell; the reference's visual design
// is not mirrored, but the content is.
func renderHTML(brand, heading, token, actionURL, expiresAtIso string) string {
	var action string
	if actionURL != "" {
		action = fmt.Sprintf(`<p><a href="%s">%s</a></p>`, html.EscapeString(actionURL), html.EscapeString(heading))
	}
	return fmt.Sprintf(
		`<!doctype html><html><body style="font-family:sans-serif;padding:24px">`+
			"<h1 style=\"font-size:18px\">%s</h1>"+
			"<p>%s</p>"+
			action+
			`<p style="font-family:monospace;font-size:16px">%s</p>`+
			`<p style="color:#666">This token expires at %s.</p>`+
			"</body></html>",
		html.EscapeString(brand),
		html.EscapeString(heading),
		html.EscapeString(token),
		html.EscapeString(expiresAtIso),
	)
}
