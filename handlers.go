// handlers.go - Complete animated sticker support with WebP tools
package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/gif"
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

// StickerHandler - Enhanced with animated WebP support
func (bot *WhatsAppBot) StickerHandler(sender types.JID, msg *events.Message) string {
	fmt.Printf("🎨 PROCESSING: Converting to sticker for +%s\n", sender.User)

	// Get image/video from message
	mediaData, mediaType, err := bot.downloadMedia(msg)
	if err != nil {
		fmt.Printf("❌ Failed to download media: %v\n", err)
		return "yah gagal download medianya nih. coba lagi ya"
	}

	fmt.Printf("📁 Media type detected: %s (%d bytes)\n", mediaType, len(mediaData))

	// Convert to sticker based on media type
	var stickerData []byte
	var isAnimated bool = false

	if mediaType == "gif" {
		// Try animated WebP first, fallback to static if failed
		stickerData, isAnimated, err = bot.convertGifToAnimatedStickerWebP(mediaData)
		if err != nil {
			fmt.Printf("⚠️ Animated conversion failed, trying static: %v\n", err)
			stickerData, err = bot.convertGifToStaticStickerWebP(mediaData)
			if err != nil {
				fmt.Printf("❌ Failed to convert GIF to sticker: %v\n", err)
				return "waduh gagal convert GIF ke sticker: " + err.Error()
			}
		}
	} else if mediaType == "video" {
		// For video files, try to convert to animated sticker
		stickerData, isAnimated, err = bot.convertVideoToAnimatedStickerWebP(mediaData)
		if err != nil {
			fmt.Printf("⚠️ Video animation failed, trying static frame: %v\n", err)
			stickerData, err = bot.convertVideoToStaticStickerWebP(mediaData)
			if err != nil {
				fmt.Printf("❌ Failed to convert video to sticker: %v\n", err)
				return "waduh gagal convert video ke sticker: " + err.Error()
			}
		}
	} else {
		// Regular image (JPEG/PNG) - always static
		stickerData, err = bot.convertToStickerWebP(mediaData)
		if err != nil {
			fmt.Printf("❌ Failed to convert image to sticker: %v\n", err)
			return "waduh gagal convert ke sticker: " + err.Error()
		}
	}

	// Send sticker with animation flag
	err = bot.sendSticker(msg.Info.Chat, stickerData, msg.Info.ID, isAnimated)
	if err != nil {
		fmt.Printf("❌ Failed to send sticker: %v\n", err)
		return "yah gagal kirim stickernya. coba lagi deh"
	}

	if isAnimated {
		fmt.Printf("✅ Animated sticker sent successfully to +%s\n", sender.User)
	} else {
		fmt.Printf("✅ Static sticker sent successfully to +%s\n", sender.User)
	}
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

// downloadMedia - Download image/video/gif from WhatsApp message with type detection
func (bot *WhatsAppBot) downloadMedia(msg *events.Message) ([]byte, string, error) {
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
		data, err := bot.client.Download(context.Background(), imageMsg)
		if err != nil {
			return nil, "", err
		}

		// Detect image type
		mediaType := "image"
		if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xD8 {
			mediaType = "jpeg"
		} else if len(data) >= 8 && string(data[1:4]) == "PNG" {
			mediaType = "png"
		} else if len(data) >= 6 && (string(data[0:6]) == "GIF87a" || string(data[0:6]) == "GIF89a") {
			mediaType = "gif"
		}

		return data, mediaType, nil

	} else if videoMsg != nil {
		fmt.Printf("📥 Downloading video/gif...\n")
		data, err := bot.client.Download(context.Background(), videoMsg)
		if err != nil {
			return nil, "", err
		}

		// Check if it's GIF (WhatsApp sometimes sends GIF as video)
		mediaType := "video"
		if len(data) >= 6 && (string(data[0:6]) == "GIF87a" || string(data[0:6]) == "GIF89a") {
			mediaType = "gif"
			fmt.Printf("🎞️ GIF detected in video message\n")
		} else {
			// Check mimetype from message
			if videoMsg.GetMimetype() == "image/gif" {
				mediaType = "gif"
				fmt.Printf("🎞️ GIF detected via mimetype\n")
			}
		}

		return data, mediaType, nil
	}

	return nil, "", fmt.Errorf("no media found in message")
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

// convertGifToAnimatedStickerWebP - Convert GIF to animated WebP sticker
func (bot *WhatsAppBot) convertGifToAnimatedStickerWebP(gifData []byte) ([]byte, bool, error) {
	fmt.Printf("🎞️ Converting GIF to animated WebP sticker...\n")

	// Check if tools are available
	if !bot.isToolAvailable("gif2webp") && !bot.isToolAvailable("ffmpeg") {
		return nil, false, fmt.Errorf("no animation tools available (gif2webp or ffmpeg needed)")
	}

	// Create temp directory
	tempDir, err := ioutil.TempDir("", "gif_animated_sticker_*")
	if err != nil {
		return nil, false, fmt.Errorf("gagal create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Validate GIF
	if len(gifData) < 6 || (string(gifData[0:6]) != "GIF87a" && string(gifData[0:6]) != "GIF89a") {
		return nil, false, fmt.Errorf("bukan format GIF yang valid")
	}

	// Try gif2webp first (Google's official tool for animated WebP)
	if bot.isToolAvailable("gif2webp") {
		return bot.convertWithGif2WebP(gifData, tempDir)
	}

	// Fallback to FFmpeg
	if bot.isToolAvailable("ffmpeg") {
		return bot.convertWithFFmpegAnimated(gifData, tempDir)
	}

	return nil, false, fmt.Errorf("no suitable animation conversion tool found")
}

// convertWithGif2WebP - Use Google's gif2webp tool for best animated WebP
func (bot *WhatsAppBot) convertWithGif2WebP(gifData []byte, tempDir string) ([]byte, bool, error) {
	fmt.Printf("🔧 Converting with gif2webp (Google's official tool)...\n")

	inputPath := filepath.Join(tempDir, "input.gif")
	outputPath := filepath.Join(tempDir, "animated.webp")

	// Save input GIF
	err := ioutil.WriteFile(inputPath, gifData, 0644)
	if err != nil {
		return nil, false, fmt.Errorf("gagal save input GIF: %v", err)
	}

	// Use gif2webp with optimized settings for WhatsApp stickers
	cmd := exec.Command("gif2webp",
		"-q", "75", // Quality 75% (good balance)
		"-m", "4", // Compression method 4 (good compression)
		"-lossy",                // Use lossy compression
		"-resize", "512", "512", // Resize to sticker dimensions
		"-mt", // Multi-threading
		inputPath,
		"-o", outputPath)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, false, fmt.Errorf("gif2webp failed: %v, output: %s", err, string(output))
	}

	// Check file size (WhatsApp has limits)
	webpData, err := ioutil.ReadFile(outputPath)
	if err != nil {
		return nil, false, fmt.Errorf("gagal read WebP output: %v", err)
	}

	// WhatsApp sticker size limit is around 500KB
	if len(webpData) > 500*1024 {
		fmt.Printf("⚠️ File too large (%d bytes), trying compressed version...\n", len(webpData))
		return bot.compressAnimatedWebP(inputPath, outputPath)
	}

	fmt.Printf("✅ Animated WebP sticker created (%d bytes)\n", len(webpData))
	return webpData, true, nil
}

// compressAnimatedWebP - Compress animated WebP if too large
func (bot *WhatsAppBot) compressAnimatedWebP(inputPath, outputPath string) ([]byte, bool, error) {
	fmt.Printf("🗜️ Compressing animated WebP for WhatsApp size limit...\n")

	// More aggressive compression
	cmd := exec.Command("gif2webp",
		"-q", "50", // Lower quality
		"-m", "6", // Max compression method
		"-lossy",
		"-resize", "480", "480", // Smaller size
		"-f", "15", // Lower frame rate
		"-mt",
		inputPath,
		"-o", outputPath)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, false, fmt.Errorf("compression failed: %v, output: %s", err, string(output))
	}

	webpData, err := ioutil.ReadFile(outputPath)
	if err != nil {
		return nil, false, err
	}

	if len(webpData) > 500*1024 {
		fmt.Printf("⚠️ Still too large after compression (%d bytes), converting to static\n", len(webpData))
		return nil, false, fmt.Errorf("file too large even after compression")
	}

	fmt.Printf("✅ Compressed animated WebP created (%d bytes)\n", len(webpData))
	return webpData, true, nil
}

// convertWithFFmpegAnimated - Use FFmpeg for animated WebP (alternative method)
func (bot *WhatsAppBot) convertWithFFmpegAnimated(gifData []byte, tempDir string) ([]byte, bool, error) {
	fmt.Printf("🔧 Converting with FFmpeg (animated WebP)...\n")

	inputPath := filepath.Join(tempDir, "input.gif")
	outputPath := filepath.Join(tempDir, "animated.webp")

	// Save input
	err := ioutil.WriteFile(inputPath, gifData, 0644)
	if err != nil {
		return nil, false, err
	}

	// FFmpeg command for animated WebP
	cmd := exec.Command("ffmpeg",
		"-i", inputPath,
		"-vcodec", "libwebp",
		"-filter:v", "fps=15,scale=512:512:force_original_aspect_ratio=decrease,pad=512:512:-1:-1:color=white@0",
		"-lossless", "0", // Lossy compression
		"-quality", "75", // Quality setting
		"-preset", "default", // Compression preset
		"-loop", "0", // Infinite loop
		"-an", // No audio
		"-y",  // Overwrite output
		outputPath)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, false, fmt.Errorf("ffmpeg failed: %v, output: %s", err, string(output))
	}

	webpData, err := ioutil.ReadFile(outputPath)
	if err != nil {
		return nil, false, err
	}

	// Check size limit
	if len(webpData) > 500*1024 {
		fmt.Printf("⚠️ FFmpeg WebP too large (%d bytes), needs compression\n", len(webpData))
		return nil, false, fmt.Errorf("animated WebP too large")
	}

	fmt.Printf("✅ FFmpeg animated WebP created (%d bytes)\n", len(webpData))
	return webpData, true, nil
}

// convertVideoToAnimatedStickerWebP - Convert video to animated sticker
func (bot *WhatsAppBot) convertVideoToAnimatedStickerWebP(videoData []byte) ([]byte, bool, error) {
	fmt.Printf("🎬 Converting video to animated sticker...\n")

	if !bot.isToolAvailable("ffmpeg") {
		return nil, false, fmt.Errorf("ffmpeg required for video to animated sticker")
	}

	tempDir, err := ioutil.TempDir("", "video_animated_sticker_*")
	if err != nil {
		return nil, false, err
	}
	defer os.RemoveAll(tempDir)

	inputPath := filepath.Join(tempDir, "input.mp4")
	outputPath := filepath.Join(tempDir, "animated.webp")

	// Save input video
	err = ioutil.WriteFile(inputPath, videoData, 0644)
	if err != nil {
		return nil, false, err
	}

	// Convert video to animated WebP with sticker optimization
	cmd := exec.Command("ffmpeg",
		"-i", inputPath,
		"-t", "10", // Limit to 10 seconds max
		"-vcodec", "libwebp",
		"-filter:v", "fps=12,scale=512:512:force_original_aspect_ratio=decrease,pad=512:512:-1:-1:color=white@0",
		"-lossless", "0",
		"-quality", "70",
		"-preset", "default",
		"-loop", "0",
		"-an",
		"-y",
		outputPath)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, false, fmt.Errorf("video conversion failed: %v, output: %s", err, string(output))
	}

	webpData, err := ioutil.ReadFile(outputPath)
	if err != nil {
		return nil, false, err
	}

	// Size check
	if len(webpData) > 500*1024 {
		return nil, false, fmt.Errorf("animated video sticker too large (%d bytes)", len(webpData))
	}

	fmt.Printf("✅ Animated video sticker created (%d bytes)\n", len(webpData))
	return webpData, true, nil
}

// convertGifToStaticStickerWebP - Fallback: convert GIF to static sticker (first frame)
func (bot *WhatsAppBot) convertGifToStaticStickerWebP(gifData []byte) ([]byte, error) {
	fmt.Printf("📸 Converting GIF to static sticker (fallback)...\n")

	// Decode GIF and extract first frame
	reader := bytes.NewReader(gifData)
	gifImg, err := gif.DecodeAll(reader)
	if err != nil {
		return nil, fmt.Errorf("gagal decode GIF: %v", err)
	}

	if len(gifImg.Image) == 0 {
		return nil, fmt.Errorf("GIF tidak punya frame")
	}

	firstFrame := gifImg.Image[0]
	stickerFrame := bot.resizeForSticker(firstFrame)

	// Create temp for conversion
	tempDir, err := ioutil.TempDir("", "gif_static_sticker_*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	tempPngPath := filepath.Join(tempDir, "frame.png")
	outputPath := filepath.Join(tempDir, "sticker.webp")

	// Save frame as PNG
	file, err := os.Create(tempPngPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	err = png.Encode(file, stickerFrame)
	if err != nil {
		return nil, err
	}

	// Convert to WebP
	return bot.convertWithCWebPTool(tempPngPath, outputPath)
}

// convertVideoToStaticStickerWebP - Extract frame from video
func (bot *WhatsAppBot) convertVideoToStaticStickerWebP(videoData []byte) ([]byte, error) {
	fmt.Printf("🎬 Converting video to static sticker (single frame)...\n")

	if !bot.isToolAvailable("ffmpeg") {
		return nil, fmt.Errorf("ffmpeg diperlukan untuk video conversion")
	}

	tempDir, err := ioutil.TempDir("", "video_static_sticker_*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)

	inputPath := filepath.Join(tempDir, "input.mp4")
	framePath := filepath.Join(tempDir, "frame.png")
	outputPath := filepath.Join(tempDir, "sticker.webp")

	// Save input
	err = ioutil.WriteFile(inputPath, videoData, 0644)
	if err != nil {
		return nil, err
	}

	// Extract single frame
	cmd := exec.Command("ffmpeg",
		"-i", inputPath,
		"-vframes", "1",
		"-f", "image2",
		"-vf", "scale=512:512:force_original_aspect_ratio=decrease,pad=512:512:-1:-1:color=white@0",
		"-y",
		framePath)

	err = cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("frame extraction failed: %v", err)
	}

	// Convert frame to WebP
	return bot.convertWithCWebPTool(framePath, outputPath)
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

// isToolAvailable - Check if external tool is available
func (bot *WhatsAppBot) isToolAvailable(toolName string) bool {
	_, err := exec.LookPath(toolName)
	return err == nil
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

// sendSticker - Enhanced with animation support
func (bot *WhatsAppBot) sendSticker(chatJID types.JID, stickerData []byte, quotedMsgID string, isAnimated bool) error {
	fmt.Printf("📤 Uploading sticker (%d bytes, animated: %v)...\n", len(stickerData), isAnimated)

	uploaded, err := bot.client.Upload(context.Background(), stickerData, whatsmeow.MediaImage)
	if err != nil {
		return fmt.Errorf("failed to upload sticker: %v", err)
	}

	// Set proper mimetype and dimensions
	var mimetype string = "image/webp"
	var width, height uint32 = 512, 512

	// Detect actual format
	if len(stickerData) >= 12 &&
		string(stickerData[0:4]) == "RIFF" &&
		string(stickerData[8:12]) == "WEBP" {
		mimetype = "image/webp"
	} else if len(stickerData) >= 8 && string(stickerData[1:4]) == "PNG" {
		mimetype = "image/png"
		// Get PNG dimensions
		reader := bytes.NewReader(stickerData)
		config, _, err := image.DecodeConfig(reader)
		if err == nil {
			width = uint32(config.Width)
			height = uint32(config.Height)
		}
	}

	// Create sticker message with animation flag
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
			IsAnimated:    proto.Bool(isAnimated), // IMPORTANT: Set animation flag
			ContextInfo: &waProto.ContextInfo{
				StanzaID: proto.String(quotedMsgID),
			},
		},
	}

	_, err = bot.client.SendMessage(context.Background(), chatJID, stickerMsg)
	if err != nil {
		return fmt.Errorf("failed to send sticker message: %v", err)
	}

	if isAnimated {
		fmt.Printf("✅ Animated sticker sent successfully\n")
	} else {
		fmt.Printf("✅ Static sticker sent successfully\n")
	}
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

// TagAllHandler - Handle tag all with corrected reply functionality and message format
func (bot *WhatsAppBot) TagAllHandler(chatJID types.JID, quotedMsgID string, quotedText string) string {
	fmt.Printf("👥 PROCESSING: Tag all members in group %s\n", chatJID.User)

	groupInfo, err := bot.client.GetGroupInfo(chatJID)
	if err != nil {
		fmt.Printf("❌ Failed to get group info: %v\n", err)
		return "yah gagal dapet info grupnya nih"
	}

	var mentions []string
	var mentionText string

	// Check if there's quoted text from the replied message
	if quotedText != "" && strings.TrimSpace(quotedText) != "" {
		fmt.Printf("📝 Using quoted message text: '%s'\n", quotedText)

		// Format: "quoted_message\n\nada pesan nih\n@mentions\ntolong dibaca ya semuanya"
		mentionText = quotedText + "\n\nada pesan nih\n"

		// Add all mentions
		for _, participant := range groupInfo.Participants {
			mentions = append(mentions, participant.JID.String())
			mentionText += fmt.Sprintf("@%s ", participant.JID.User)
		}

		mentionText += "\ntolong dibaca ya semuanya!!"

	} else {
		fmt.Printf("📝 No quoted text, using default tagall message\n")

		// Default format when no specific message is quoted
		mentionText = "halo semuanyaa ada yang penting nih\n\n"

		// Add all mentions
		for _, participant := range groupInfo.Participants {
			mentions = append(mentions, participant.JID.String())
			mentionText += fmt.Sprintf("@%s ", participant.JID.User)
		}

		mentionText += "\nkok tag semua? ada apa emang yaa?"
	}

	// Send message with reply to the original message
	msg := &waProto.Message{
		ExtendedTextMessage: &waProto.ExtendedTextMessage{
			Text: proto.String(mentionText),
			ContextInfo: &waProto.ContextInfo{
				MentionedJID: mentions,
				StanzaID:     proto.String(quotedMsgID),      // Reply to original message
				Participant:  proto.String(chatJID.String()), // Important for groups
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

// checkWebPToolsAvailability - Enhanced with animation tools check
func (bot *WhatsAppBot) checkWebPToolsAvailability() {
	fmt.Printf("🔍 Checking WebP and media tools availability...\n")

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

	// Check gif2webp (IMPORTANT for animated stickers)
	if _, err := exec.LookPath("gif2webp"); err == nil {
		fmt.Printf("✅ gif2webp found (animated stickers enabled)\n")
	} else {
		fmt.Printf("❌ gif2webp not found (animated stickers disabled)\n")
	}

	// Check ImageMagick
	if _, err := exec.LookPath("convert"); err == nil {
		fmt.Printf("✅ ImageMagick convert found\n")
	} else {
		fmt.Printf("❌ ImageMagick convert not found\n")
	}

	// Check FFmpeg for GIF/video processing
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		fmt.Printf("✅ FFmpeg found (GIF/video support enabled)\n")
		// Check libwebp support
		cmd := exec.Command("ffmpeg", "-codecs")
		output, err := cmd.CombinedOutput()
		if err == nil && strings.Contains(string(output), "libwebp") {
			fmt.Printf("✅ FFmpeg with libwebp support detected\n")
		} else {
			fmt.Printf("⚠️ FFmpeg found but libwebp support unclear\n")
		}
	} else {
		fmt.Printf("❌ FFmpeg not found (GIF/video processing limited)\n")
	}

	// Installation instructions
	fmt.Printf("\n💡 To install media tools:\n")
	fmt.Printf("Ubuntu/Debian: sudo apt-get install webp imagemagick ffmpeg\n")
	fmt.Printf("macOS: brew install webp imagemagick ffmpeg\n")
	fmt.Printf("Windows: Download from respective official sites\n")
	fmt.Printf("\n🎞️ For BEST animated sticker support:\n")
	fmt.Printf("- gif2webp (part of webp package) - ESSENTIAL\n")
	fmt.Printf("- ffmpeg with libwebp - Alternative method\n")
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
