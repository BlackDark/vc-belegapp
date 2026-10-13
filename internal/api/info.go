package api

import "github.com/BlackDark/vc-belegapp/internal/config"

// Build is what the binary knows about itself: the values stamped in at link
// time plus the Typst it found on the machine.
type Build struct {
	Version      string
	Commit       string
	BuildDatum   string
	TypstVersion string
}

// NewInfo maps the process configuration onto what /system/info may report.
//
// This is the one place that decision is made, so it is the one place a test
// can check it: a configured instance passes every host, folder, user name,
// password, webhook address and token through here, and none of them has a
// field on the other side. Adding one — a MailHost, a NotifyWebhookURL — is
// therefore a change to this function, and the absence test in
// system_info_test.go fails on it.
//
// The three intake switches are the whole of the intake information. They come
// from the *Configured gates rather than the raw switches, because each gate is
// the condition that actually starts a ticker or enqueues a job: that is what
// "is this intake running" means. The gate itself is a boolean and cannot leak.
func NewInfo(cfg config.Config, build Build) Info {
	return Info{
		Version:               build.Version,
		Commit:                build.Commit,
		BuildDatum:            build.BuildDatum,
		StorageBackend:        cfg.StorageBackend,
		ErkennungKonfiguriert: cfg.LLMEnabled && !cfg.OpenAIKeyMissing(),
		LLMModel:              cfg.LLMModel,
		LLMBaseURL:            cfg.LLMBaseURL,
		LLMEnabled:            cfg.LLMEnabled,
		TypstVersion:          build.TypstVersion,
		MailEnabled:           cfg.MailConfigured(),
		NotifyEnabled:         cfg.NotifyConfigured(),
		WatchEnabled:          cfg.WatchConfigured(),
	}
}
