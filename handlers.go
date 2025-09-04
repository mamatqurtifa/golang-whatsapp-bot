// handlers.go - Fixed version with proper reply functionality and corrected tagall format
package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// StickerHandler - Handle sticker conversion with WebP tools
func (bot *WhatsAppBot) StickerHandler(sender types.JID, msg *events.Message) string {
	fmt.Printf("🎨 PROCESSING: Converting to sticker for +%s\n", sender.User)

	// Get image from message
	imageData, err := bot.downloadImage(msg)
	if err != nil {
		fmt.Printf("❌ Failed to download image: %v\n", err)
		return "yah gagal download gambarnya nih. coba lagi ya"
	}

	// Convert to sticker (with WebP tools)
	stickerData, err := bot.convertToStickerWebP(imageData)
	if err != nil {
		fmt.Printf("❌ Failed to convert to sticker: %v\n", err)
		return "waduh gagal convert ke sticker: " + err.Error()
	}

	// Send sticker
	err = bot.sendSticker(msg.Info.Chat, stickerData, msg.Info.ID)
	if err != nil {
		fmt.Printf("❌ Failed to send sticker: %v\n", err)
		return "yah gagal kirim stickernya. coba lagi deh"
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
		return "yah gagal download stickernya. coba lagi ya"
	}

	// Convert to image (PNG)
	imageData, err := bot.convertStickerToImageWebP(stickerData)
	if err != nil {
		fmt.Printf("❌ Failed to convert to image: %v\n", err)
		return "waduh gagal convert ke gambar nih"
	}

	// Send image
	err = bot.sendImage(msg.Info.Chat, imageData, "converted_image.png", msg.Info.ID)
	if err != nil {
		fmt.Printf("❌ Failed to send image: %v\n", err)
		return "yah gagal kirim gambarnya. coba lagi deh"
	}

	fmt.Printf("✅ Image sent successfully to +%s\n", sender.User)
	return ""
}

// TagAllHandler - Handle tag all with corrected reply functionality and message format
func (bot *WhatsAppBot) TagAllHandler(chatJID types.JID, quotedMsgID string, originalText string) string {
	fmt.Printf("👥 PROCESSING: Tag all members in group %s\n", chatJID.User)

	groupInfo, err := bot.client.GetGroupInfo(chatJID)
	if err != nil {
		fmt.Printf("❌ Failed to get group info: %v\n", err)
		return "yah gagal dapet info grupnya nih"
	}

	var mentions []string

	// Format pesan berdasarkan input
	var mentionText string

	if originalText != "" && strings.ToLower(strings.TrimSpace(originalText)) != "/tagall" {
		// Jika ada pesan setelah /tagall, gunakan format: "pesan_user\nada pesan nih @mentions"
		mentionText = originalText + "\nada pesan nih "
	} else {
		// Jika hanya "/tagall", gunakan format default
		mentionText = "halo semuanyaa ada yang penting nih\n"
	}

	// Tambahkan semua mentions
	for _, participant := range groupInfo.Participants {
		mentions = append(mentions, participant.JID.String())
		mentionText += fmt.Sprintf("@%s ", participant.JID.User)
	}

	// Tambahkan penutup sesuai konteks
	if originalText != "" && strings.ToLower(strings.TrimSpace(originalText)) != "/tagall" {
		mentionText += "\ntolong dibaca ya semuanya"
	} else {
		mentionText += "\nkok tag semua? ada apa emang?"
	}

	// Kirim pesan dengan reply ke pesan asli
	msg := &waProto.Message{
		ExtendedTextMessage: &waProto.ExtendedTextMessage{
			Text: proto.String(mentionText),
			ContextInfo: &waProto.ContextInfo{
				MentionedJID: mentions,
				StanzaID:     proto.String(quotedMsgID),      // Reply ke pesan asli
				Participant:  proto.String(chatJID.String()), // Penting untuk grup
				QuotedMessage: &waProto.Message{
					Conversation: proto.String("tagall"),
				},
			},
		},
	}

	_, err = bot.client.SendMessage(context.Background(), chatJID, msg)
	if err != nil {
		fmt.Printf("❌ Failed to send mention message: %v\n", err)
		return "yah gagal kirim mention. coba lagi deh"
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

// convertToStickerWebP - Convert image to WebP sticker using cwebp tool
func (bot *WhatsAppBot) convertToStickerWebP(imageData []byte) ([]byte, error) {
	fmt.Printf("🔄 Converting to WebP sticker format...\n")

	// Check if already WebP
	if len(imageData) >= 12 &&
		string(imageData[0:4]) == "RIFF" &&
		string(imageData[8:12]) == "WEBP" {
		fmt.Printf("✅ Already WebP format - optimizing for sticker...\n")
		return bot.optimizeWebPSticker(imageData)
	}

	// Create temp directory
	tempDir, err := ioutil.TempDir("", "sticker_convert_*")
	if err != nil {
		return nil, fmt.Errorf("gagal create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	inputPath := filepath.Join(tempDir, "input")
	outputPath := filepath.Join(tempDir, "sticker.webp")

	// Detect format and decode
	var img image.Image
	reader := bytes.NewReader(imageData)

	if len(imageData) >= 2 && imageData[0] == 0xFF && imageData[1] == 0xD8 {
		fmt.Printf("📸 JPEG detected\n")
		img, err = jpeg.Decode(reader)
		inputPath += ".jpg"
	} else if len(imageData) >= 8 && string(imageData[1:4]) == "PNG" {
		fmt.Printf("🖼️ PNG detected\n")
		img, err = png.Decode(reader)
		inputPath += ".png"
	} else {
		return nil, fmt.Errorf("format ga didukung. cuma JPG/PNG aja")
	}

	if err != nil {
		return nil, fmt.Errorf("gagal decode gambar: %v", err)
	}

	// Resize to proper sticker dimensions
	stickerImg := bot.resizeForSticker(img)

	// Save temporary PNG for conversion
	tempPngPath := filepath.Join(tempDir, "temp.png")
	file, err := os.Create(tempPngPath)
	if err != nil {
		return nil, fmt.Errorf("gagal create temp file: %v", err)
	}

	err = png.Encode(file, stickerImg)
	file.Close()
	if err != nil {
		return nil, fmt.Errorf("gagal save temp PNG: %v", err)
	}

	// Convert to WebP using cwebp tool
	webpData, err := bot.convertWithCWebPTool(tempPngPath, outputPath)
	if err != nil {
		fmt.Printf("⚠️ cwebp failed, trying fallback methods...\n")

		// Try ImageMagick as fallback
		webpData, err = bot.convertWithImageMagickTool(tempPngPath, outputPath)
		if err != nil {
			fmt.Printf("⚠️ All WebP tools failed, using PNG fallback\n")
			// Fallback to PNG (still works as sticker)
			return bot.createPNGSticker(stickerImg)
		}
	}

	fmt.Printf("✅ WebP sticker created (%d bytes)\n", len(webpData))
	return webpData, nil
}

// resizeForSticker - Resize image to proper sticker dimensions (512x512 max, maintain aspect ratio)
func (bot *WhatsAppBot) resizeForSticker(src image.Image) image.Image {
	srcBounds := src.Bounds()
	srcWidth := srcBounds.Dx()
	srcHeight := srcBounds.Dy()

	// Calculate dimensions maintaining aspect ratio
	var newWidth, newHeight int
	if srcWidth > srcHeight {
		newWidth = 512
		newHeight = int(float64(srcHeight) * 512.0 / float64(srcWidth))
	} else {
		newHeight = 512
		newWidth = int(float64(srcWidth) * 512.0 / float64(srcHeight))
	}

	// Create new image with calculated dimensions
	dst := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))

	// Simple resize
	for y := 0; y < newHeight; y++ {
		for x := 0; x < newWidth; x++ {
			srcX := int(float64(x) * float64(srcWidth) / float64(newWidth))
			srcY := int(float64(y) * float64(srcHeight) / float64(newHeight))
			dst.Set(x, y, src.At(srcBounds.Min.X+srcX, srcBounds.Min.Y+srcY))
		}
	}

	fmt.Printf("✅ Resized to %dx%d (sticker dimensions)\n", newWidth, newHeight)
	return dst
}

// convertWithCWebPTool - Convert using Google's cwebp command line tool
func (bot *WhatsAppBot) convertWithCWebPTool(inputPath, outputPath string) ([]byte, error) {
	fmt.Printf("🔧 Converting with cwebp tool...\n")

	// Check if cwebp is available
	_, err := exec.LookPath("cwebp")
	if err != nil {
		return nil, fmt.Errorf("cwebp not found: %v", err)
	}

	// Run cwebp with sticker-optimized settings
	cmd := exec.Command("cwebp",
		"-q", "80", // Quality 80%
		"-preset", "picture", // Picture preset
		"-resize", "512", "512", // Resize to 512x512
		"-crop", "512", "512", "0", "0", // Crop if needed
		inputPath,
		"-o", outputPath)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("cwebp command failed: %v, output: %s", err, string(output))
	}

	// Check if output file was created
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("cwebp did not create output file")
	}

	// Read the WebP file
	webpData, err := ioutil.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read WebP output: %v", err)
	}

	fmt.Printf("✅ cwebp conversion successful (%d bytes)\n", len(webpData))
	return webpData, nil
}

// convertWithImageMagickTool - Convert using ImageMagick convert
func (bot *WhatsAppBot) convertWithImageMagickTool(inputPath, outputPath string) ([]byte, error) {
	fmt.Printf("🔧 Converting with ImageMagick...\n")

	// Check if convert is available
	_, err := exec.LookPath("convert")
	if err != nil {
		return nil, fmt.Errorf("imagemagick convert not found: %v", err)
	}

	// Run convert with sticker settings
	cmd := exec.Command("convert",
		inputPath,
		"-resize", "512x512>", // Resize maintaining aspect ratio, max 512x512
		"-background", "transparent", // Transparent background
		"-gravity", "center", // Center the image
		"-extent", "512x512", // Extend canvas to exactly 512x512
		"-quality", "80", // Set quality
		outputPath)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("imagemagick failed: %v, output: %s", err, string(output))
	}

	// Read result
	webpData, err := ioutil.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read ImageMagick output: %v", err)
	}

	fmt.Printf("✅ ImageMagick conversion successful (%d bytes)\n", len(webpData))
	return webpData, nil
}

// optimizeWebPSticker - Optimize existing WebP for sticker use
func (bot *WhatsAppBot) optimizeWebPSticker(webpData []byte) ([]byte, error) {
	fmt.Printf("🔧 Optimizing existing WebP for sticker...\n")

	tempDir, err := ioutil.TempDir("", "webp_optimize_*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	inputPath := filepath.Join(tempDir, "input.webp")
	outputPath := filepath.Join(tempDir, "optimized.webp")

	// Save input WebP
	err = ioutil.WriteFile(inputPath, webpData, 0644)
	if err != nil {
		return nil, err
	}

	// Optimize with cwebp
	cmd := exec.Command("cwebp",
		inputPath,
		"-q", "80",
		"-resize", "512", "512",
		"-o", outputPath)

	err = cmd.Run()
	if err != nil {
		// If optimization fails, return original
		fmt.Printf("⚠️ WebP optimization failed, using original\n")
		return webpData, nil
	}

	// Read optimized version
	optimizedData, err := ioutil.ReadFile(outputPath)
	if err != nil {
		return webpData, nil // Return original on read error
	}

	fmt.Printf("✅ WebP optimized (%d -> %d bytes)\n", len(webpData), len(optimizedData))
	return optimizedData, nil
}

// createPNGSticker - Create PNG sticker as fallback
func (bot *WhatsAppBot) createPNGSticker(img image.Image) ([]byte, error) {
	fmt.Printf("📦 Creating PNG sticker (fallback mode)...\n")

	var buf bytes.Buffer
	err := png.Encode(&buf, img)
	if err != nil {
		return nil, err
	}

	fmt.Printf("✅ PNG sticker ready (%d bytes)\n", buf.Len())
	return buf.Bytes(), nil
}

// convertStickerToImageWebP - Convert sticker to image with WebP support
func (bot *WhatsAppBot) convertStickerToImageWebP(stickerData []byte) ([]byte, error) {
	fmt.Printf("🔄 Converting sticker to image...\n")

	// Check if it's WebP
	if len(stickerData) >= 12 &&
		string(stickerData[0:4]) == "RIFF" &&
		string(stickerData[8:12]) == "WEBP" {

		fmt.Printf("🎯 WebP sticker detected - converting to PNG...\n")
		return bot.webpToPNG(stickerData)
	}

	// Handle other formats
	var img image.Image
	var err error
	reader := bytes.NewReader(stickerData)

	if len(stickerData) >= 8 && string(stickerData[1:4]) == "PNG" {
		fmt.Printf("🖼️ PNG sticker detected\n")
		img, err = png.Decode(reader)
	} else if len(stickerData) >= 2 && stickerData[0] == 0xFF && stickerData[1] == 0xD8 {
		fmt.Printf("📸 JPEG sticker detected\n")
		img, err = jpeg.Decode(reader)
	} else {
		fmt.Printf("⚠️ Unknown format - returning as-is\n")
		return stickerData, nil
	}

	if err != nil {
		fmt.Printf("⚠️ Decode failed - returning original: %v\n", err)
		return stickerData, nil
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

// webpToPNG - Convert WebP to PNG using dwebp tool
func (bot *WhatsAppBot) webpToPNG(webpData []byte) ([]byte, error) {
	tempDir, err := ioutil.TempDir("", "webp_to_png_*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	inputPath := filepath.Join(tempDir, "input.webp")
	outputPath := filepath.Join(tempDir, "output.png")

	// Save WebP input
	err = ioutil.WriteFile(inputPath, webpData, 0644)
	if err != nil {
		return nil, err
	}

	// Try dwebp first
	err = bot.convertWebPWithDWebP(inputPath, outputPath)
	if err != nil {
		// Try ImageMagick as fallback
		err = bot.convertWebPWithImageMagick(inputPath, outputPath)
		if err != nil {
			return nil, fmt.Errorf("all WebP conversion tools failed: %v", err)
		}
	}

	// Read PNG result
	pngData, err := ioutil.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read PNG output: %v", err)
	}

	return pngData, nil
}

// convertWebPWithDWebP - Use dwebp tool
func (bot *WhatsAppBot) convertWebPWithDWebP(inputPath, outputPath string) error {
	_, err := exec.LookPath("dwebp")
	if err != nil {
		return fmt.Errorf("dwebp not found")
	}

	cmd := exec.Command("dwebp", inputPath, "-o", outputPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("dwebp failed: %v, output: %s", err, string(output))
	}

	fmt.Printf("✅ dwebp conversion successful\n")
	return nil
}

// convertWebPWithImageMagick - Use ImageMagick for WebP to PNG
func (bot *WhatsAppBot) convertWebPWithImageMagick(inputPath, outputPath string) error {
	_, err := exec.LookPath("convert")
	if err != nil {
		return fmt.Errorf("imagemagick not found")
	}

	cmd := exec.Command("convert", inputPath, outputPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("imagemagick failed: %v, output: %s", err, string(output))
	}

	fmt.Printf("✅ ImageMagick WebP conversion successful\n")
	return nil
}

// sendSticker - Send sticker with accurate format detection
func (bot *WhatsAppBot) sendSticker(chatJID types.JID, stickerData []byte, quotedMsgID string) error {
	fmt.Printf("📤 Uploading sticker (%d bytes)...\n", len(stickerData))

	uploaded, err := bot.client.Upload(context.Background(), stickerData, whatsmeow.MediaImage)
	if err != nil {
		return fmt.Errorf("failed to upload sticker: %v", err)
	}

	// Accurate format detection
	var mimetype string
	var width, height uint32 = 512, 512

	if len(stickerData) >= 12 &&
		string(stickerData[0:4]) == "RIFF" &&
		string(stickerData[8:12]) == "WEBP" {
		mimetype = "image/webp"
		fmt.Printf("🎯 Sending WebP sticker\n")
	} else if len(stickerData) >= 8 && string(stickerData[1:4]) == "PNG" {
		mimetype = "image/png"
		fmt.Printf("🎯 Sending PNG sticker\n")

		// For PNG, try to get actual dimensions
		reader := bytes.NewReader(stickerData)
		config, _, err := image.DecodeConfig(reader)
		if err == nil {
			width = uint32(config.Width)
			height = uint32(config.Height)
		}
	} else if len(stickerData) >= 2 && stickerData[0] == 0xFF && stickerData[1] == 0xD8 {
		mimetype = "image/jpeg"
		fmt.Printf("🎯 Sending JPEG sticker\n")
	} else {
		mimetype = "image/png" // Safe default
		fmt.Printf("🎯 Unknown format, defaulting to PNG\n")
	}

	// Create sticker message
	stickerMsg := &waProto.Message{
		StickerMessage: &waProto.StickerMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String(mimetype),
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(stickerData))),
			Width:         proto.Uint32(width),
			Height:        proto.Uint32(height),
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

	fmt.Printf("✅ Sticker sent successfully (format: %s)\n", mimetype)
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
			Caption:       proto.String("udah ku jadiin gambar nih"),
			ContextInfo: &waProto.ContextInfo{
				StanzaID: proto.String(quotedMsgID),
			},
		},
	}

	_, err = bot.client.SendMessage(context.Background(), chatJID, imageMsg)
	if err != nil {
		return fmt.Errorf("failed to send image message: %v", err)
	}

	fmt.Printf("✅ Image sent successfully\n")
	return nil
}

// checkWebPToolsAvailability - Check if WebP tools are installed
func (bot *WhatsAppBot) checkWebPToolsAvailability() {
	fmt.Printf("🔍 Checking WebP tools availability...\n")

	// Check cwebp
	if _, err := exec.LookPath("cwebp"); err == nil {
		fmt.Printf("✅ cwebp found\n")
	} else {
		fmt.Printf("❌ cwebp not found\n")
	}

	// Check dwebp
	if _, err := exec.LookPath("dwebp"); err == nil {
		fmt.Printf("✅ dwebp found\n")
	} else {
		fmt.Printf("❌ dwebp not found\n")
	}

	// Check ImageMagick
	if _, err := exec.LookPath("convert"); err == nil {
		fmt.Printf("✅ ImageMagick convert found\n")
	} else {
		fmt.Printf("❌ ImageMagick convert not found\n")
	}

	// Installation instructions
	fmt.Printf("\n💡 To install WebP tools:\n")
	fmt.Printf("Ubuntu/Debian: sudo apt-get install webp imagemagick\n")
	fmt.Printf("macOS: brew install webp imagemagick\n")
	fmt.Printf("Windows: Download from https://developers.google.com/speed/webp/download\n")
	fmt.Printf("========================================\n")
}

// Helper function
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func init() {
	rand.Seed(time.Now().UnixNano())
}
