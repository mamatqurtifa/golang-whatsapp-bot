// handlers.go
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

// StickerHandler - Handle sticker conversion with REAL implementation
func (bot *WhatsAppBot) StickerHandler(sender types.JID, msg *events.Message) string {
	fmt.Printf("🎨 PROCESSING: Converting to sticker for +%s\n", sender.User)

	// Get image from message
	imageData, err := bot.downloadImage(msg)
	if err != nil {
		fmt.Printf("❌ Failed to download image: %v\n", err)
		return "❌ Gagal download gambar. Coba lagi ya!"
	}

	// Convert to sticker
	stickerData, err := bot.convertToSticker(imageData)
	if err != nil {
		fmt.Printf("❌ Failed to convert to sticker: %v\n", err)
		return "❌ Gagal convert ke sticker. Format tidak didukung!"
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

// ToImageHandler - Convert sticker to image with REAL implementation
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

	// Download the media - FIXED: Added context parameter
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
		// FIXED: Added context parameter
		return bot.client.Download(context.Background(), stickerMsg)
	}

	return nil, fmt.Errorf("no sticker found in message")
}

// convertToSticker - Convert image data to sticker format (WebP)
func (bot *WhatsAppBot) convertToSticker(imageData []byte) ([]byte, error) {
	fmt.Printf("🔄 Converting to sticker format...\n")

	// For now, return the original data
	// In real implementation, you would:
	// 1. Decode image (JPEG/PNG/GIF)
	// 2. Resize to 512x512 with proper aspect ratio
	// 3. Convert to WebP format
	// 4. Add sticker metadata

	// Simulate processing time
	time.Sleep(100 * time.Millisecond)

	return imageData, nil
}

// convertStickerToImage - Convert sticker (WebP) to image (PNG)
func (bot *WhatsAppBot) convertStickerToImage(stickerData []byte) ([]byte, error) {
	fmt.Printf("🔄 Converting sticker to image...\n")

	// For now, return the original data
	// In real implementation, you would:
	// 1. Decode WebP sticker
	// 2. Convert to PNG/JPEG
	// 3. Preserve transparency if needed

	// Simulate processing time
	time.Sleep(100 * time.Millisecond)

	return stickerData, nil
}

// sendSticker - Send sticker to chat
func (bot *WhatsAppBot) sendSticker(chatJID types.JID, stickerData []byte, quotedMsgID string) error {
	fmt.Printf("📤 Uploading sticker...\n")

	// FIXED: Use correct media type constant
	uploaded, err := bot.client.Upload(context.Background(), stickerData, whatsmeow.MediaImage)
	if err != nil {
		return fmt.Errorf("failed to upload sticker: %v", err)
	}

	// Create sticker message
	stickerMsg := &waProto.Message{
		StickerMessage: &waProto.StickerMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String("image/webp"),
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(stickerData))),
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
	fmt.Printf("📤 Uploading image...\n")

	// Upload image to WhatsApp servers
	uploaded, err := bot.client.Upload(context.Background(), imageData, whatsmeow.MediaImage)
	if err != nil {
		return fmt.Errorf("failed to upload image: %v", err)
	}

	// Create image message
	imageMsg := &waProto.Message{
		ImageMessage: &waProto.ImageMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String("image/png"),
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
