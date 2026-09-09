// Package http builds the application's net/http ServeMux from the
// Stripe service and session store.
package http

import (
	"database/sql"
	stdhttp "net/http"
	"strings"

	"github.com/iustin94/makerspace/api/internal/captcha"
	"github.com/iustin94/makerspace/api/internal/email"
	"github.com/iustin94/makerspace/api/internal/errpage"
	"github.com/iustin94/makerspace/api/internal/session"
	"github.com/iustin94/makerspace/api/internal/stripe"
)

// Mux composes the route table.
//
// The application has two runtimes split by Host:
//
//   - Apex (e.g. development.makerspace.olaru.dk) — the marketing site,
//     served as static Hugo HTML via the file-server fallback. Only the
//     Stripe webhook is dynamic on this host (it doesn't need the cookie).
//
//   - Members (e.g. members.development.makerspace.olaru.dk) — every
//     auth-touching surface: dashboard, login, checkout, profile, billing.
//     Same Go binary, different Host. ServeMux's host-prefixed patterns
//     route requests by their Host header. When MembersHost is empty,
//     routes register without a host prefix (any-host) so tests and local
//     stand-alone runs still work.
func Mux(
	staticDir string,
	publicURL string,
	membersHost string,
	db *sql.DB,
	sessions *session.Store,
	mailer *email.Mailer,
	svc *stripe.Service,
	captchaSvc *captcha.Service,
) *stdhttp.ServeMux {
	mux := stdhttp.NewServeMux()

	// onMembers prefixes a pattern with the members Host so it only matches
	// the member portal subdomain. If membersHost is empty the prefix is
	// dropped — pattern stays any-host.
	onMembers := func(pattern string) string {
		if membersHost == "" {
			return pattern
		}
		// Patterns can include a method prefix: "GET /foo". Insert host between
		// the method and the path.
		// Safe approach: split on first space.
		for i := 0; i < len(pattern); i++ {
			if pattern[i] == ' ' {
				return pattern[:i+1] + membersHost + pattern[i+1:]
			}
		}
		// No method prefix — bare path.
		return membersHost + pattern
	}

	staticFallback := staticFallbackWith404(staticDir, membersHost)

	// ───────────────────────── apex (any host with no more-specific match) ──
	// Stripe webhook — Stripe POSTs to whatever URL is configured in the
	// dashboard. We accept it on any host so configuration mistakes don't
	// drop events silently.
	mux.HandleFunc("/webhook", svc.HandleWebhook)

	// Altcha captcha challenge endpoint. Any-host so the same Hugo form can
	// fetch it whether served from members or apex. No auth, no side effects.
	mux.HandleFunc("GET /captcha/challenge", captchaSvc.ServeChallenge())

	// Membership-price fragment. Any-host so the apex-rendered /checkout/
	// page can htmx-load it same-origin (no CORS, no cross-origin cert
	// prompts in local-dev). Public data — STRIPE_PRICE_ID drives output,
	// no session required.
	mux.HandleFunc("GET /checkout/prices", svc.ServePrices())

	// ───────────────────────── members runtime (host-prefixed) ──────────────
	// Dashboard — backend-rendered page with auth-aware chrome.
	mux.HandleFunc(onMembers("GET /dashboard"), svc.ServeDashboardPage())
	mux.HandleFunc(onMembers("GET /da/dashboard"), svc.ServeDashboardPage())

	// Dashboard fragments (htmx-loaded into the page)
	mux.HandleFunc(onMembers("GET /dashboard/hero"), svc.ServeHero())
	mux.HandleFunc(onMembers("/subscriptions"), svc.ServeSubscriptions())
	mux.HandleFunc(onMembers("/cancel-subscription"), svc.CancelSubscription())
	mux.HandleFunc(onMembers("POST /reactivate-subscription"), svc.ReactivateSubscription())
	mux.HandleFunc(onMembers("GET /cancel-modal"), svc.ServeCancelModal())
	mux.HandleFunc(onMembers("GET /reactivate-modal"), svc.ServeReactivateModal())
	mux.HandleFunc(onMembers("GET /cancel-modal/dismiss"), svc.DismissModal())

	// Profile + password
	mux.HandleFunc(onMembers("GET /profile"), svc.ServeProfile())
	mux.HandleFunc(onMembers("POST /profile"), svc.UpdateProfile())
	mux.HandleFunc(onMembers("GET /profile/edit"), svc.ServeProfileEdit())
	mux.HandleFunc(onMembers("GET /profile/password"), svc.ServePassword())
	mux.HandleFunc(onMembers("POST /profile/password"), svc.UpdatePassword())

	// Billing
	mux.HandleFunc(onMembers("GET /invoices"), svc.ServeInvoices())
	mux.HandleFunc(onMembers("GET /billing-portal"), svc.BillingPortal())

	// Checkout — signup form (Hugo page) + form handler. Any-host so the
	// apex-rendered /checkout/ page can submit same-origin; failure redirects
	// (relative URLs) stay on whichever host the user is actually on, no host
	// hop on validation errors. /checkout/prices is already any-host above.
	mux.Handle("GET /checkout/", sessions.RedirectIfLoggedIn("/dashboard", staticFallback))
	mux.HandleFunc("POST /checkout/", svc.CreateCheckoutSession())
	// Re-subscribe from the dashboard. POST-only and a dedicated handler: it's a
	// state-initiating action for an authenticated member, kept apart from the
	// anonymous-signup POST above (no form, no captcha, dashboard-bound errors).
	mux.HandleFunc("POST /re-checkout", svc.ReCheckout())
	mux.Handle("GET /da/checkout/", sessions.RedirectIfLoggedIn("/dashboard", staticFallback))

	// Stripe redirect target after successful payment. Handler verifies the
	// session_id, runs fulfillment idempotently (covers the redirect-beats-
	// webhook race), then 303-redirects to the login page so the user signs
	// in with the credentials they just set on the signup form. Any-host so
	// Stripe can be configured to return to whichever origin the user came
	// from (the SuccessURL is built server-side from BACKEND_PUBLIC_URL).
	mux.HandleFunc("GET /checkout/success", svc.ServeCheckoutSuccess())
	mux.HandleFunc("GET /da/checkout/success", svc.ServeCheckoutSuccess())

	// Auth flows. Login routes are members-only: the marketing site bakes
	// absolute https://members.X/login/ links at Hugo build time (from the
	// SUBDOMAIN/TOPDOMAIN env vars forwarded into the Hugo build), so
	// users never hit apex/login/ in normal navigation. Restricting these
	// to the members host means the cookie lands on the right origin and
	// the post-login /dashboard redirect resolves correctly without any
	// backend redirect indirection.
	mux.HandleFunc("/logout", sessions.Logout)
	mux.Handle(onMembers("GET /login/"), sessions.RedirectIfLoggedIn("/dashboard", staticFallback))
	mux.Handle(onMembers("GET /da/login/"), sessions.RedirectIfLoggedIn("/dashboard", staticFallback))
	mux.HandleFunc(onMembers("POST /login/"), sessions.Login(db))
	mux.HandleFunc(onMembers("POST /request-reset"), session.RequestReset(db, mailer, publicURL, captchaSvc))
	mux.HandleFunc(onMembers("POST /reset-password/"), session.ResetPassword(db))
	mux.HandleFunc(onMembers("POST /reset-password"), session.ResetPassword(db))

	// ───────────────────────── apex static fallback (any host) ──────────────
	// Serves Hugo-built marketing site. Registered last so member routes win
	// for the members host. The apex host has no more-specific patterns, so
	// every request falls here.
	mux.Handle("/", staticFallback)
	return mux
}

// staticFallbackWith404 wraps http.FileServer so that requests to a path
// that doesn't exist on disk get the Hugo-built public/404.html body with
// proper HTTP 404 status — instead of the default plain-text
// "404 page not found\n" that FileServer emits. Localization (Danish
// /da/* paths) is handled inside errpage.Render.
//
// The members host serves the same Hugo tree so login, checkout and the
// dashboard skeletons resolve there, but nothing on that host is meant to
// be indexed: every response carries X-Robots-Tag: noindex so search
// engines stop listing members.<domain> as a duplicate of the apex.
func staticFallbackWith404(staticDir string, membersHost string) stdhttp.Handler {
	dir := stdhttp.Dir(staticDir)
	fs := stdhttp.FileServer(dir)
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		f, err := dir.Open(r.URL.Path)
		if err != nil {
			errpage.Render(w, r, staticDir, "404", stdhttp.StatusNotFound)
			return
		}
		f.Close()
		if isMembersHost(r.Host, membersHost) {
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		}
		w.Header().Set("Cache-Control", cacheControlFor(r.URL.Path))
		fs.ServeHTTP(w, r)
	})
}

func isMembersHost(requestHost string, membersHost string) bool {
	if membersHost == "" {
		return false
	}
	hostWithoutPort, _, _ := strings.Cut(requestHost, ":")
	return strings.EqualFold(hostWithoutPort, membersHost)
}

// cacheControlFor picks a caching policy from the URL shape alone.
//
// Hugo image processing writes content-hashed names ("_hu_<hash>"), so
// those can be cached for a year and marked immutable. Fonts change only
// with a deliberate vendor bump, so a week is safe. Everything else — HTML,
// the unhashed styles.css, klaro-config.js — is revalidated on every
// request; FileServer answers If-Modified-Since with a 304, so the cost is
// a round trip, not a transfer.
func cacheControlFor(path string) string {
	if strings.Contains(path, "_hu_") {
		return "public, max-age=31536000, immutable"
	}
	if strings.HasPrefix(path, "/fonts/") {
		return "public, max-age=604800"
	}
	return "public, no-cache"
}
