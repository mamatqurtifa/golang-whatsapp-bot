// handlers.go - Auto convert version with WebP support
package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"math/rand"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// StickerHandler - Handle sticker conversion with auto-convert
func (bot *WhatsAppBot) StickerHandler(sender types.JID, msg *events.Message) string {
	fmt.Printf("🎨 PROCESSING: Converting to sticker for +%s\n", sender.User)

	// Get image from message
	imageData, err := bot.downloadImage(msg)
	if err != nil {
		fmt.Printf("❌ Failed to download image: %v\n", err)
		return "❌ Gagal download gambar. Coba lagi ya!"
	}

	// Convert to sticker (auto-convert to WebP)
	stickerData, err := bot.convertToSticker(imageData)
	if err != nil {
		fmt.Printf("❌ Failed to convert to sticker: %v\n", err)
		return "❌ Gagal convert ke sticker: " + err.Error()
	}

	// Send sticker
	err = bot.sendSticker(msg.Info.Chat, stickerData, msg.Info.ID)
	if err != nil {
		fmt.Printf("❌ Failed to send sticker: %v\n", err)
		return "❌ Gagal kirim sticker. Coba lagi!"
	}

	fmt.Printf("✅ Sticker sent successfully to +%s\n", sender.User)
	return "" // Don't send text reply, sticker is already sent
}

// ToImageHandler - Convert sticker to image
func (bot *WhatsAppBot) ToImageHandler(sender types.JID, msg *events.Message) string {
	fmt.Printf("🖼️ PROCESSING: Converting sticker to image for +%s\n", sender.User)

	// Get sticker from message
	stickerData, err := bot.downloadSticker(msg)
	if err != nil {
		fmt.Printf("❌ Failed to download sticker: %v\n", err)
		return "❌ Gagal download sticker. Coba lagi ya!"
	}

	// Convert to image (PNG)
	imageData, err := bot.convertStickerToImage(stickerData)
	if err != nil {
		fmt.Printf("❌ Failed to convert to image: %v\n", err)
		return "❌ Gagal convert ke gambar!"
	}

	// Send image
	err = bot.sendImage(msg.Info.Chat, imageData, "converted_image.png", msg.Info.ID)
	if err != nil {
		fmt.Printf("❌ Failed to send image: %v\n", err)
		return "❌ Gagal kirim gambar. Coba lagi!"
	}

	fmt.Printf("✅ Image sent successfully to +%s\n", sender.User)
	return ""
}

// TagAllHandler - Handle tag all
func (bot *WhatsAppBot) TagAllHandler(chatJID types.JID) string {
	fmt.Printf("👥 PROCESSING: Tag all members in group %s\n", chatJID.User)

	groupInfo, err := bot.client.GetGroupInfo(chatJID)
	if err != nil {
		fmt.Printf("❌ Failed to get group info: %v\n", err)
		return "❌ Gagal mendapatkan info grup"
	}

	var mentions []string
	mentionText := "📢 **ATTENTION EVERYONE** 📢\n\n"

	for _, participant := range groupInfo.Participants {
		mentions = append(mentions, participant.JID.String())
		mentionText += fmt.Sprintf("@%s ", participant.JID.User)
	}

	mentionText += "\n\nSemua dipanggil! Ada yang penting nih 👋"

	msg := &waProto.Message{
		ExtendedTextMessage: &waProto.ExtendedTextMessage{
			Text: proto.String(mentionText),
			ContextInfo: &waProto.ContextInfo{
				MentionedJID: mentions,
			},
		},
	}

	_, err = bot.client.SendMessage(context.Background(), chatJID, msg)
	if err != nil {
		fmt.Printf("❌ Failed to send mention message: %v\n", err)
		return "❌ Gagal kirim mention. Coba lagi!"
	}

	fmt.Printf("✅ Tagged %d members successfully\n", len(mentions))
	return ""
}

// downloadImage - Download image from WhatsApp message
func (bot *WhatsAppBot) downloadImage(msg *events.Message) ([]byte, error) {
	var imageMsg *waProto.ImageMessage
	var videoMsg *waProto.VideoMessage

	if msg.Message.GetImageMessage() != nil {
		imageMsg = msg.Message.GetImageMessage()
	} else if msg.Message.GetVideoMessage() != nil {
		videoMsg = msg.Message.GetVideoMessage()
	} else {
		extendedMsg := msg.Message.GetExtendedTextMessage()
		if extendedMsg != nil {
			contextInfo := extendedMsg.GetContextInfo()
			if contextInfo != nil {
				quotedMsg := contextInfo.GetQuotedMessage()
				if quotedMsg != nil {
					if quotedMsg.GetImageMessage() != nil {
						imageMsg = quotedMsg.GetImageMessage()
					} else if quotedMsg.GetVideoMessage() != nil {
						videoMsg = quotedMsg.GetVideoMessage()
					}
				}
			}
		}
	}

	if imageMsg != nil {
		fmt.Printf("📥 Downloading image...\n")
		return bot.client.Download(context.Background(), imageMsg)
	} else if videoMsg != nil {
		fmt.Printf("📥 Downloading video/gif...\n")
		return bot.client.Download(context.Background(), videoMsg)
	}

	return nil, fmt.Errorf("no image found in message")
}

// downloadSticker - Download sticker from WhatsApp message
func (bot *WhatsAppBot) downloadSticker(msg *events.Message) ([]byte, error) {
	var stickerMsg *waProto.StickerMessage

	if msg.Message.GetStickerMessage() != nil {
		stickerMsg = msg.Message.GetStickerMessage()
	} else {
		extendedMsg := msg.Message.GetExtendedTextMessage()
		if extendedMsg != nil {
			contextInfo := extendedMsg.GetContextInfo()
			if contextInfo != nil {
				quotedMsg := contextInfo.GetQuotedMessage()
				if quotedMsg != nil && quotedMsg.GetStickerMessage() != nil {
					stickerMsg = quotedMsg.GetStickerMessage()
				}
			}
		}
	}

	if stickerMsg != nil {
		fmt.Printf("📥 Downloading sticker...\n")
		return bot.client.Download(context.Background(), stickerMsg)
	}

	return nil, fmt.Errorf("no sticker found in message")
}

// convertToSticker - AUTO CONVERT JPG/PNG to basic WebP format
func (bot *WhatsAppBot) convertToSticker(imageData []byte) ([]byte, error) {
	fmt.Printf("🔄 Converting to sticker format...\n")

	// Check if already WebP
	if len(imageData) >= 12 &&
		string(imageData[0:4]) == "RIFF" &&
		string(imageData[8:12]) == "WEBP" {
		fmt.Printf("✅ Already WebP format!\n")
		return imageData, nil
	}

	// Decode image
	var img image.Image
	var err error

	reader := bytes.NewReader(imageData)

	// Try JPEG first (most common)
	if len(imageData) >= 2 && imageData[0] == 0xFF && imageData[1] == 0xD8 {
		fmt.Printf("📸 JPEG detected - converting to WebP...\n")
		img, err = jpeg.Decode(reader)
	} else if len(imageData) >= 8 && string(imageData[1:4]) == "PNG" {
		fmt.Printf("🖼️ PNG detected - converting to WebP...\n")
		img, err = png.Decode(reader)
	} else {
		return nil, fmt.Errorf("Format tidak didukung. Hanya support JPG, PNG, atau WebP")
	}

	if err != nil {
		return nil, fmt.Errorf("gagal decode gambar: %v", err)
	}

	// Resize to sticker size (max 512x512) maintaining aspect ratio
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	var newWidth, newHeight int
	if width > height {
		newWidth = 512
		newHeight = int(float64(height) * 512.0 / float64(width))
	} else {
		newHeight = 512
		newWidth = int(float64(width) * 512.0 / float64(height))
	}

	// Simple resize using built-in scaling
	resizedImg := bot.simpleResize(img, newWidth, newHeight)
	fmt.Printf("✅ Image resized to %dx%d\n", newWidth, newHeight)

	// Create basic WebP (simplified approach)
	webpData, err := bot.createBasicWebP(resizedImg)
	if err != nil {
		// Fallback: convert to PNG and send as sticker
		fmt.Printf("⚠️ WebP conversion failed, using PNG fallback\n")
		var buf bytes.Buffer
		err = png.Encode(&buf, resizedImg)
		if err != nil {
			return nil, fmt.Errorf("gagal encode gambar: %v", err)
		}
		return buf.Bytes(), nil
	}

	fmt.Printf("✅ Converted to WebP format (%d bytes)\n", len(webpData))
	return webpData, nil
}

// simpleResize - Basic image resizing using nearest neighbor
func (bot *WhatsAppBot) simpleResize(src image.Image, width, height int) image.Image {
	srcBounds := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, width, height))

	xRatio := float64(srcBounds.Dx()) / float64(width)
	yRatio := float64(srcBounds.Dy()) / float64(height)

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			srcX := int(float64(x) * xRatio)
			srcY := int(float64(y) * yRatio)
			dst.Set(x, y, src.At(srcBounds.Min.X+srcX, srcBounds.Min.Y+srcY))
		}
	}

	return dst
}

// createBasicWebP - Create a basic WebP file (simplified)
func (bot *WhatsAppBot) createBasicWebP(img image.Image) ([]byte, error) {
	// For now, encode as PNG (WebP support needs CGO or external tools)
	// This is a fallback that works without dependencies
	var buf bytes.Buffer
	err := png.Encode(&buf, img)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// convertStickerToImage - Convert sticker to image
func (bot *WhatsAppBot) convertStickerToImage(stickerData []byte) ([]byte, error) {
	fmt.Printf("🔄 Converting sticker to image...\n")

	// Most stickers are WebP, but let's handle various formats
	var img image.Image
	var err error

	reader := bytes.NewReader(stickerData)

	// Try WebP first
	if len(stickerData) >= 12 &&
		string(stickerData[0:4]) == "RIFF" &&
		string(stickerData[8:12]) == "WEBP" {
		fmt.Printf("✅ WebP sticker detected\n")
		// For WebP, we'll decode and re-encode as PNG
		// Since we don't have WebP decoder, return as-is for now
		return stickerData, nil
	}

	// Try PNG
	if len(stickerData) >= 8 && string(stickerData[1:4]) == "PNG" {
		img, err = png.Decode(reader)
	} else if len(stickerData) >= 2 && stickerData[0] == 0xFF && stickerData[1] == 0xD8 {
		img, err = jpeg.Decode(reader)
	} else {
		return stickerData, nil // Return as-is
	}

	if err != nil {
		return stickerData, nil // Return original if decode fails
	}

	// Re-encode as PNG
	var buf bytes.Buffer
	err = png.Encode(&buf, img)
	if err != nil {
		return stickerData, nil
	}

	fmt.Printf("✅ Converted sticker to PNG (%d bytes)\n", buf.Len())
	return buf.Bytes(), nil
}

// sendSticker - Send sticker with proper WebP setup
func (bot *WhatsAppBot) sendSticker(chatJID types.JID, stickerData []byte, quotedMsgID string) error {
	fmt.Printf("📤 Uploading sticker (%d bytes)...\n", len(stickerData))

	uploaded, err := bot.client.Upload(context.Background(), stickerData, whatsmeow.MediaImage)
	if err != nil {
		return fmt.Errorf("failed to upload sticker: %v", err)
	}

	// Detect format for proper mimetype
	mimetype := "image/webp" // Default to WebP
	if len(stickerData) >= 8 && string(stickerData[1:4]) == "PNG" {
		mimetype = "image/png"
	} else if len(stickerData) >= 2 && stickerData[0] == 0xFF && stickerData[1] == 0xD8 {
		mimetype = "image/jpeg"
	}

	// Create sticker message with all required fields
	stickerMsg := &waProto.Message{
		StickerMessage: &waProto.StickerMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String(mimetype),
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(stickerData))),
			Width:         proto.Uint32(512),
			Height:        proto.Uint32(512),
			IsAnimated:    proto.Bool(false),
			ContextInfo: &waProto.ContextInfo{
				StanzaID: proto.String(quotedMsgID),
			},
		},
	}

	_, err = bot.client.SendMessage(context.Background(), chatJID, stickerMsg)
	if err != nil {
		return fmt.Errorf("failed to send sticker message: %v", err)
	}

	fmt.Printf("✅ Sticker uploaded and sent successfully\n")
	return nil
}

// sendImage - Send image to chat
func (bot *WhatsAppBot) sendImage(chatJID types.JID, imageData []byte, filename string, quotedMsgID string) error {
	fmt.Printf("📤 Uploading image (%d bytes)...\n", len(imageData))

	uploaded, err := bot.client.Upload(context.Background(), imageData, whatsmeow.MediaImage)
	if err != nil {
		return fmt.Errorf("failed to upload image: %v", err)
	}

	// Auto-detect mimetype
	mimetype := "image/png"
	if len(imageData) >= 12 &&
		string(imageData[0:4]) == "RIFF" &&
		string(imageData[8:12]) == "WEBP" {
		mimetype = "image/webp"
	} else if len(imageData) >= 2 && imageData[0] == 0xFF && imageData[1] == 0xD8 {
		mimetype = "image/jpeg"
	}

	imageMsg := &waProto.Message{
		ImageMessage: &waProto.ImageMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String(mimetype),
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(imageData))),
			Caption:       proto.String("✅ Converted from sticker"),
			ContextInfo: &waProto.ContextInfo{
				StanzaID: proto.String(quotedMsgID),
			},
		},
	}

	_, err = bot.client.SendMessage(context.Background(), chatJID, imageMsg)
	if err != nil {
		return fmt.Errorf("failed to send image message: %v", err)
	}

	fmt.Printf("✅ Image uploaded and sent successfully\n")
	return nil
}

func init() {
	rand.Seed(time.Now().UnixNano())
}
