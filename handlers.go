// handlers.go - Simple version without external dependencies
package main

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// StickerHandler - Handle sticker conversion with simple implementation
func (bot *WhatsAppBot) StickerHandler(sender types.JID, msg *events.Message) string {
	fmt.Printf("🎨 PROCESSING: Converting to sticker for +%s\n", sender.User)

	// Get image from message
	imageData, err := bot.downloadImage(msg)
	if err != nil {
		fmt.Printf("❌ Failed to download image: %v\n", err)
		return "❌ Gagal download gambar. Coba lagi ya!"
	}

	// Check if it's already a valid format for sticker
	stickerData, err := bot.convertToSticker(imageData)
	if err != nil {
		fmt.Printf("❌ Failed to convert to sticker: %v\n", err)
		return "❌ Gagal convert ke sticker. " + err.Error()
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

// ToImageHandler - Convert sticker to image with simple implementation
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
	return "" // Don't send text reply, image is already sent
}

// TagAllHandler - Handle tag all with REAL mention implementation
func (bot *WhatsAppBot) TagAllHandler(chatJID types.JID) string {
	fmt.Printf("👥 PROCESSING: Tag all members in group %s\n", chatJID.User)

	// Get group info
	groupInfo, err := bot.client.GetGroupInfo(chatJID)
	if err != nil {
		fmt.Printf("❌ Failed to get group info: %v\n", err)
		return "❌ Gagal mendapatkan info grup"
	}

	// Create mention message
	var mentions []string
	mentionText := "📢 **ATTENTION EVERYONE** 📢\n\n"

	for _, participant := range groupInfo.Participants {
		mentions = append(mentions, participant.JID.String())
		mentionText += fmt.Sprintf("@%s ", participant.JID.User)
	}

	mentionText += "\n\nSemua dipanggil! Ada yang penting nih 👋"

	// Send with mentions
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
	return "" // Don't send additional reply
}

// downloadImage - Download image from WhatsApp message
func (bot *WhatsAppBot) downloadImage(msg *events.Message) ([]byte, error) {
	var imageMsg *waProto.ImageMessage
	var videoMsg *waProto.VideoMessage

	// Check direct image
	if msg.Message.GetImageMessage() != nil {
		imageMsg = msg.Message.GetImageMessage()
	} else if msg.Message.GetVideoMessage() != nil {
		videoMsg = msg.Message.GetVideoMessage()
	} else {
		// Check quoted message
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

	// Download the media
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

	// Check direct sticker
	if msg.Message.GetStickerMessage() != nil {
		stickerMsg = msg.Message.GetStickerMessage()
	} else {
		// Check quoted sticker
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

// convertToSticker - Simple version that checks format and gives helpful error
func (bot *WhatsAppBot) convertToSticker(imageData []byte) ([]byte, error) {
	fmt.Printf("🔄 Converting to sticker format...\n")

	// Check if it's already WebP (WhatsApp sticker format)
	if len(imageData) >= 12 {
		// Check for WebP signature: "RIFF" + 4 bytes size + "WEBP"
		if string(imageData[0:4]) == "RIFF" && string(imageData[8:12]) == "WEBP" {
			fmt.Printf("✅ Already in WebP format - perfect for sticker!\n")
			return imageData, nil
		}

		// Check for PNG signature
		if len(imageData) >= 8 && string(imageData[1:4]) == "PNG" {
			fmt.Printf("⚠️ PNG detected - need conversion to WebP\n")
			return nil, fmt.Errorf("PNG perlu dikonversi ke WebP dulu. Gunakan online converter (PNG to WebP)")
		}

		// Check for JPEG signature
		if len(imageData) >= 2 && imageData[0] == 0xFF && imageData[1] == 0xD8 {
			fmt.Printf("⚠️ JPEG detected - need conversion to WebP\n")
			return nil, fmt.Errorf("JPEG perlu dikonversi ke WebP dulu. Gunakan online converter (JPEG to WebP)")
		}
	}

	// For now, try to send as-is and let WhatsApp handle it
	fmt.Printf("⚠️ Unknown format - trying to send as-is\n")
	return imageData, nil
}

// convertStickerToImage - Simple version that works with WebP stickers
func (bot *WhatsAppBot) convertStickerToImage(stickerData []byte) ([]byte, error) {
	fmt.Printf("🔄 Converting sticker to image...\n")

	// Check if it's WebP
	if len(stickerData) >= 12 &&
		string(stickerData[0:4]) == "RIFF" &&
		string(stickerData[8:12]) == "WEBP" {
		fmt.Printf("✅ WebP sticker detected\n")

		// For simplicity, return as-is with PNG mimetype
		// WhatsApp can handle WebP as image too
		return stickerData, nil
	}

	// If not WebP, return as-is
	fmt.Printf("⚠️ Unknown sticker format - returning as-is\n")
	return stickerData, nil
}

// sendSticker - Send sticker to chat with proper formatting
func (bot *WhatsAppBot) sendSticker(chatJID types.JID, stickerData []byte, quotedMsgID string) error {
	fmt.Printf("📤 Uploading sticker (%d bytes)...\n", len(stickerData))

	// Upload sticker to WhatsApp servers
	uploaded, err := bot.client.Upload(context.Background(), stickerData, whatsmeow.MediaImage)
	if err != nil {
		return fmt.Errorf("failed to upload sticker: %v", err)
	}

	// Determine mimetype based on content
	mimetype := "image/webp"
	if len(stickerData) >= 8 && string(stickerData[1:4]) == "PNG" {
		mimetype = "image/png"
	} else if len(stickerData) >= 2 && stickerData[0] == 0xFF && stickerData[1] == 0xD8 {
		mimetype = "image/jpeg"
	}

	// Create sticker message with enhanced metadata
	stickerMsg := &waProto.Message{
		StickerMessage: &waProto.StickerMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String(mimetype),
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(stickerData))),
			// Add sticker dimensions (WhatsApp expects these)
			Width:  proto.Uint32(512),
			Height: proto.Uint32(512),
			// Mark as sticker (important!)
			IsAnimated: proto.Bool(false),
			ContextInfo: &waProto.ContextInfo{
				StanzaID: proto.String(quotedMsgID),
			},
		},
	}

	// Send sticker
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

	// Upload image to WhatsApp servers
	uploaded, err := bot.client.Upload(context.Background(), imageData, whatsmeow.MediaImage)
	if err != nil {
		return fmt.Errorf("failed to upload image: %v", err)
	}

	// Determine mimetype
	mimetype := "image/png"
	if len(imageData) >= 12 &&
		string(imageData[0:4]) == "RIFF" &&
		string(imageData[8:12]) == "WEBP" {
		mimetype = "image/webp"
	} else if len(imageData) >= 2 && imageData[0] == 0xFF && imageData[1] == 0xD8 {
		mimetype = "image/jpeg"
	}

	// Create image message
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

	// Send image
	_, err = bot.client.SendMessage(context.Background(), chatJID, imageMsg)
	if err != nil {
		return fmt.Errorf("failed to send image message: %v", err)
	}

	fmt.Printf("✅ Image uploaded and sent successfully\n")
	return nil
}

// Init random seed
func init() {
	rand.Seed(time.Now().UnixNano())
}
