package adminui

import (
	"fmt"
	"log/slog"
	"net/http"
)

const (
	msgConfigFormatsLoadFailed = "load admin configuration formats failed"
	msgConfigFormatsSaveFailed = "save admin configuration formats failed"
)

type formatSettingsForm struct {
	Action   string
	CSRF     string
	Settings FormatSettings
}

func (c *Console) loadConfigFormats(r *http.Request, data *configPageData) {
	if c.crawlFormats == nil {
		return
	}
	data.FormatsOn = true
	settings, err := c.crawlFormats.CurrentFormats(r.Context())
	if err != nil {
		slog.WarnContext(r.Context(), msgConfigFormatsLoadFailed, slog.Any("error", err))
		data.FormatsError = "Document format settings are unavailable."

		return
	}
	data.Formats = &settings
	data.FormatForm = &formatSettingsForm{
		Action:   configPath + "/formats#panel-crawler",
		CSRF:     data.CSRF,
		Settings: settings,
	}
}

func (c *Console) handleConfigFormats(w http.ResponseWriter, r *http.Request) {
	if c.config == nil || c.crawlFormats == nil {
		http.NotFound(w, r)

		return
	}
	settings, err := formatSettingsFromRequest(r)
	if err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)

		return
	}
	if err := c.crawlFormats.SaveFormats(r.Context(), settings); err != nil {
		slog.WarnContext(r.Context(), msgConfigFormatsSaveFailed, slog.Any("error", err))
		data := c.configPage(r, "", "")
		data.FormatsSaveError = "Saving format settings failed."
		c.render(r.Context(), w, c.tpl.config, "layout", data)

		return
	}
	http.Redirect(
		w,
		r,
		configPath+"?saved=formats#panel-crawler",
		http.StatusSeeOther,
	)
}

func formatSettingsFromRequest(r *http.Request) (FormatSettings, error) {
	if err := r.ParseForm(); err != nil {
		return FormatSettings{}, fmt.Errorf("parse document format settings: %w", err)
	}

	return FormatSettings{
		Text:     r.PostForm.Get("text") == "on",
		XMLFeeds: r.PostForm.Get("xmlfeeds") == "on",
		PDF:      r.PostForm.Get("pdf") == "on",
		Office:   r.PostForm.Get("office") == "on",
		Images:   r.PostForm.Get("images") == "on",
		Audio:    r.PostForm.Get("audio") == "on",
		Misc:     r.PostForm.Get("misc") == "on",
		Archives: r.PostForm.Get("archives") == "on",
	}, nil
}
