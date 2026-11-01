package chatturn

import (
	"net/url"
	"strings"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// SetAttachmentPublicURL uses the existing public reverse-proxy origin for local storage URLs.
func (t *Turn) SetAttachmentPublicURL(publicURL string) { t.deps.AttachmentPublicURL = publicURL }

// contentWithAttachments is used only in model context, never as visible/stored message text.
func contentWithAttachments(text string, attachments []domain.Attachment, publicURL string) string {
	var content strings.Builder
	content.WriteString(text)
	for _, attachment := range attachments {
		if attachment.Type != "image" {
			continue
		}
		imageURL := attachment.URL
		if parsed, err := url.Parse(imageURL); err == nil && !parsed.IsAbs() {
			if base, err := url.Parse(publicURL); err == nil && base.IsAbs() {
				imageURL = base.ResolveReference(parsed).String()
			}
		}
		content.WriteString("\n\nUser-provided image URL for the requested action: ")
		content.WriteString(imageURL)
	}
	return content.String()
}
