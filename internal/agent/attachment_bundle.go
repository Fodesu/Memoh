package agent

import (
	"github.com/memohai/memoh/internal/agent/tools"
	attachmentpkg "github.com/memohai/memoh/internal/attachment"
)

func bundleFromToolAttachment(att tools.Attachment) attachmentpkg.Bundle {
	return attachmentpkg.Bundle{
		Type:        att.Type,
		Base64:      att.Base64,
		Path:        att.Path,
		URL:         att.URL,
		PlatformKey: att.PlatformKey,
		ContentHash: att.ContentHash,
		Name:        att.Name,
		Mime:        att.Mime,
		Size:        att.Size,
		Metadata:    att.Metadata,
	}.Normalize()
}

// fileAttachmentFromBundle converts a normalized bundle to an agent FileAttachment.
// Callers must guarantee bundle is already normalized (produced by BundleFromXxx or Normalize()).
func fileAttachmentFromBundle(bundle attachmentpkg.Bundle) FileAttachment {
	return FileAttachment{
		Type:        bundle.Type,
		Base64:      bundle.Base64,
		Path:        bundle.Path,
		URL:         bundle.URL,
		PlatformKey: bundle.PlatformKey,
		Mime:        bundle.Mime,
		Name:        bundle.Name,
		ContentHash: bundle.ContentHash,
		Size:        bundle.Size,
		Metadata:    bundle.Metadata,
	}
}

func fileAttachmentFromToolAttachment(att tools.Attachment) FileAttachment {
	return fileAttachmentFromBundle(bundleFromToolAttachment(att))
}

func toolAttachmentFromFileAttachment(att FileAttachment) tools.Attachment {
	return tools.Attachment{
		Type:        att.Type,
		Base64:      att.Base64,
		Path:        att.Path,
		URL:         att.URL,
		PlatformKey: att.PlatformKey,
		Mime:        att.Mime,
		Name:        att.Name,
		ContentHash: att.ContentHash,
		Size:        att.Size,
		Metadata:    att.Metadata,
	}
}

func toolAttachmentsFromFileAttachments(atts []FileAttachment) []tools.Attachment {
	if len(atts) == 0 {
		return nil
	}
	out := make([]tools.Attachment, 0, len(atts))
	for _, att := range atts {
		out = append(out, toolAttachmentFromFileAttachment(att))
	}
	return out
}
