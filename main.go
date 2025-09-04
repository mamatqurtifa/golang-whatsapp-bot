// main.go - Updated version with proper reply functionality
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

type WhatsAppBot struct {
	client            *whatsmeow.Client
	rateLimiter       chan struct{}
	wg                sync.WaitGroup
	mutex             sync.RWMutex
	processedMessages int64
	startTime         time.Time
}

func NewWhatsAppBot() *WhatsAppBot {
	dbLog := waLog.Stdout("Database", "ERROR", false)
	container, err := sqlstore.New(context.Background(), "sqlite3", "file:session.db?_foreign_keys=on", dbLog)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		log.Fatal("Failed to get device:", err)
	}

	clientLog := waLog.Stdout("Client", "ERROR", false)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	return &WhatsAppBot{
		client:      client,
		rateLimiter: make(chan struct{}, 50), // Increased rate limit
		startTime:   time.Now(),
	}
}

func (bot *WhatsAppBot) Start() {
	fmt.Println("🤖 WhatsApp Bot - WebP Enhanced Edition")
	fmt.Println("========================================")

	// Check WebP tools availability
	bot.checkWebPToolsAvailability()

	bot.client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Message:
			go bot.handleMessage(v)
		case *events.Connected:
			phoneNumber := "Unknown"
			if bot.client.Store.ID != nil {
				phoneNumber = "+" + bot.client.Store.ID.User
			}
			fmt.Printf("✅ Connected to WhatsApp | Number: %s\n", phoneNumber)
		case *events.Disconnected:
			fmt.Println("❌ Disconnected from WhatsApp")
		case *events.LoggedOut:
			fmt.Println("🚪 Logged out from WhatsApp - Session expired")
		}
	})

	if bot.client.Store.ID == nil {
		fmt.Println("No existing session found")
		fmt.Println("Scan QR code with WhatsApp:")
		fmt.Println("================================")

		qrChan, _ := bot.client.GetQRChannel(context.Background())
		err := bot.client.Connect()
		if err != nil {
			log.Fatal("Failed to connect:", err)
		}

		for evt := range qrChan {
			if evt.Event == "code" {
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
				fmt.Println()
				fmt.Println("Scan this QR code with WhatsApp > Linked Devices > Link a Device")
			} else if evt.Event == "success" {
				fmt.Println("✅ QR Code login successful")
				break
			} else {
				fmt.Printf("QR Channel event: %s\n", evt.Event)
			}
		}
	} else {
		phoneNumber := "+" + bot.client.Store.ID.User
		fmt.Printf("Existing session found for: %s\n", phoneNumber)
		fmt.Println("Connecting...")

		err := bot.client.Connect()
		if err != nil {
			log.Fatal("Failed to connect:", err)
		}
	}

	fmt.Println("🤖 Bot ready and listening...")

	if bot.client.Store.ID != nil {
		phoneNumber := "+" + bot.client.Store.ID.User
		fmt.Printf("📱 Logged in as: %s\n", phoneNumber)
	}

	fmt.Println("========================================")

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	fmt.Println("Shutting down...")
	bot.wg.Wait()
	bot.client.Disconnect()
	fmt.Println("Bye")
}

func (bot *WhatsAppBot) handleMessage(msg *events.Message) {
	bot.wg.Add(1)
	defer bot.wg.Done()

	if msg.Info.IsFromMe {
		return
	}

	bot.rateLimiter <- struct{}{}
	defer func() { <-bot.rateLimiter }()

	// Extract message text from different message types
	messageText := bot.extractMessageText(msg)
	sender := msg.Info.Sender
	chatJID := msg.Info.Chat
	isGroup := strings.Contains(chatJID.String(), "@g.us")

	// Get sender display name
	senderName := sender.User
	if len(senderName) > 12 {
		senderName = senderName[:12] + "..."
	}

	// Enhanced chat type display
	chatType := "💬 DM"
	chatInfo := ""
	if isGroup {
		chatType = "👥 GROUP"
		// Get group name if possible
		groupName := chatJID.User
		if len(groupName) > 15 {
			groupName = groupName[:15] + "..."
		}
		chatInfo = fmt.Sprintf(" (%s)", groupName)
	}

	// Enhanced message logging with proper timestamp
	timestamp := time.Now().Format("15:04:05")
	fmt.Printf("\n📨 [%s] %s%s\n", timestamp, chatType, chatInfo)
	fmt.Printf("👤 From: +%s\n", sender.User)
	fmt.Printf("💬 Message: '%s'\n", messageText)

	bot.mutex.Lock()
	bot.processedMessages++
	currentCount := bot.processedMessages
	bot.mutex.Unlock()

	fmt.Printf("📊 Total processed: %d\n", currentCount)

	// Check if it's a command
	if messageText != "" && strings.HasPrefix(messageText, "/") {
		fmt.Printf("⚡ Processing command: %s\n", strings.Split(messageText, " ")[0])
		bot.processCommand(chatJID, sender, messageText, isGroup, msg)
	} else if messageText == "" {
		fmt.Printf("⚠️ Empty message received - might be media/unsupported type\n")
	} else {
		fmt.Printf("💭 Regular message (not a command)\n")
	}

	fmt.Println("----------------------------------------")
}

// extractMessageText - Extract text from different message types
func (bot *WhatsAppBot) extractMessageText(msg *events.Message) string {
	// Try different message types
	if msg.Message.GetConversation() != "" {
		return msg.Message.GetConversation()
	}

	if extendedMsg := msg.Message.GetExtendedTextMessage(); extendedMsg != nil {
		return extendedMsg.GetText()
	}

	if imageMsg := msg.Message.GetImageMessage(); imageMsg != nil {
		return imageMsg.GetCaption()
	}

	if videoMsg := msg.Message.GetVideoMessage(); videoMsg != nil {
		return videoMsg.GetCaption()
	}

	if stickerMsg := msg.Message.GetStickerMessage(); stickerMsg != nil {
		return "[Sticker]"
	}

	if audioMsg := msg.Message.GetAudioMessage(); audioMsg != nil {
		return "[Audio]"
	}

	if documentMsg := msg.Message.GetDocumentMessage(); documentMsg != nil {
		return fmt.Sprintf("[Document: %s]", documentMsg.GetFileName())
	}

	return ""
}

func (bot *WhatsAppBot) processCommand(chatJID, sender types.JID, command string, isGroup bool, originalMsg *events.Message) {
	parts := strings.Split(strings.TrimSpace(command), " ")
	if len(parts) == 0 {
		return
	}

	cmd := strings.ToLower(parts[0])
	var response string

	fmt.Printf("🔍 Command detected: '%s'\n", cmd)
	startTime := time.Now()

	switch cmd {
	case "/hi":
		response = `halo! 👋

Commands:
/help - bantuan lengkap
/hi - sapa bot  
/sticker atau /s - gambar ke stiker (WebP)
/toimg - stiker ke gambar
/tagall - mention semua (grup only)
/calendar - tanggal hari ini
/stats - statistik bot
/tools - cek WebP tools

bot siap melayani nih! 🤖`
		fmt.Printf("✅ Responding to /hi command\n")

	case "/help":
		response = `🤖 WhatsApp Bot Helper - WebP Edition

aku bot yang bisa bantu convert sticker dengan WebP!

📋 Commands:
• /hi - menu utama
• /sticker atau /s - konversi gambar/gif ke stiker WebP 
• /toimg - konversi stiker ke gambar PNG
• /tagall - mention semua member (grup only)
• /calendar - info tanggal hari ini
• /stats - statistik bot
• /tools - cek status WebP tools

🎯 Features:
• WebP sticker support (cwebp/dwebp)
• auto-resize ke 512x512
• fallback ke PNG kalo WebP gagal
• support JPEG, PNG, WebP input

simple tapi works! 😌`
		fmt.Printf("✅ Responding to /help command\n")

	case "/tools":
		response = bot.getToolsStatus()
		fmt.Printf("✅ Responding to /tools command\n")

	case "/s", "/sticker":
		if bot.hasQuotedImage(originalMsg) {
			response = bot.StickerHandler(sender, originalMsg)
		} else {
			response = "reply gambar atau gif dulu dong biar bisa dijadiin stiker WebP"
		}
		fmt.Printf("✅ Responding to sticker command\n")

	case "/toimg":
		if bot.hasQuotedSticker(originalMsg) {
			response = bot.ToImageHandler(sender, originalMsg)
		} else {
			response = "reply stiker dulu biar bisa dikonversi ke gambar"
		}
		fmt.Printf("✅ Responding to toimg command\n")

	case "/calendar":
		now := time.Now()
		response = fmt.Sprintf(`📅 *%s*
🕐 %s WIB
📊 hari ke-%d tahun %d
🗓️ minggu ke-%d

semoga harimu menyenangkan ya! 😊`,
			now.Format("Monday, 2 January 2006"),
			now.Format("15:04:05"),
			now.YearDay(),
			now.Year(),
			getWeekOfYear(now))
		fmt.Printf("✅ Responding to calendar command\n")

	case "/tagall":
		if isGroup {
			// Extract the full message text after /tagall command
			fullText := bot.extractMessageText(originalMsg)
			if strings.Contains(fullText, "/tagall ") {
				// Get text after "/tagall "
				afterCommand := strings.TrimSpace(strings.SplitN(fullText, "/tagall ", 2)[1])
				response = bot.TagAllHandler(chatJID, originalMsg.Info.ID, afterCommand)
			} else {
				// Just "/tagall" without additional text
				response = bot.TagAllHandler(chatJID, originalMsg.Info.ID, "")
			}
		} else {
			response = "command /tagall cuma bisa dipake di grup ya"
		}
		fmt.Printf("✅ Responding to tagall command\n")

	case "/stats":
		bot.mutex.RLock()
		count := bot.processedMessages
		bot.mutex.RUnlock()
		uptime := time.Since(bot.startTime)

		// Calculate messages per minute
		minutes := uptime.Minutes()
		msgPerMin := float64(0)
		if minutes > 0 {
			msgPerMin = float64(count) / minutes
		}

		response = fmt.Sprintf(`📊 *Bot Statistics*

💬 pesan diproses: *%d*
⏱️ uptime: *%v*
📈 rata-rata: *%.1f* msg/menit
⚡ mode: WebP + concurrent processing
🚀 response time: < 500ms
📱 status: online & ready

keep chatting! 🤖`,
			count,
			uptime.Truncate(time.Second),
			msgPerMin)
		fmt.Printf("✅ Responding to stats command\n")

	default:
		fmt.Printf("❓ Unknown command: %s\n", cmd)
		return // No response for unknown commands
	}

	// Send reply ONLY if there's a response and it's NOT empty
	if response != "" {
		fmt.Printf("📝 Preparing response (%d chars)\n", len(response))

		// Send reply immediately with proper context
		go func() {
			bot.sendReply(chatJID, response, originalMsg.Info.ID, sender)

			processingTime := time.Since(startTime)
			senderShort := sender.User
			if len(senderShort) > 10 {
				senderShort = senderShort[:10] + "..."
			}
			fmt.Printf("✅ REPLIED to %s: %s (took %v)\n", senderShort, cmd, processingTime)
		}()
	} else {
		fmt.Printf("ℹ️ No text response - media/action already sent\n")
	}
}

// getToolsStatus - Get WebP tools installation status
func (bot *WhatsAppBot) getToolsStatus() string {
	status := "🔧 *WebP Tools Status*\n\n"

	// Check cwebp
	if bot.isToolAvailable("cwebp") {
		status += "✅ cwebp: installed\n"
	} else {
		status += "❌ cwebp: not found\n"
	}

	// Check dwebp
	if bot.isToolAvailable("dwebp") {
		status += "✅ dwebp: installed\n"
	} else {
		status += "❌ dwebp: not found\n"
	}

	// Check ImageMagick
	if bot.isToolAvailable("convert") {
		status += "✅ ImageMagick: installed\n"
	} else {
		status += "❌ ImageMagick: not found\n"
	}

	status += "\n💡 *install commands:*\n"
	status += "ubuntu: `sudo apt install webp imagemagick`\n"
	status += "macOS: `brew install webp imagemagick`\n"
	status += "windows: download WebP tools dari google\n\n"

	if bot.isToolAvailable("cwebp") {
		status += "🎯 WebP stickers: *enabled*"
	} else {
		status += "⚠️ WebP stickers: *fallback mode* (PNG)"
	}

	return status
}

// isToolAvailable - Check if external tool is available
func (bot *WhatsAppBot) isToolAvailable(toolName string) bool {
	_, err := exec.LookPath(toolName)
	return err == nil
}

// Helper function to get week of year
func getWeekOfYear(t time.Time) int {
	_, week := t.ISOWeek()
	return week
}

func (bot *WhatsAppBot) hasQuotedImage(msg *events.Message) bool {
	// Check direct image
	if msg.Message.GetImageMessage() != nil {
		return true
	}

	// Check direct video/gif
	if msg.Message.GetVideoMessage() != nil {
		return true
	}

	// Check extended text message (replies)
	extendedMsg := msg.Message.GetExtendedTextMessage()
	if extendedMsg != nil {
		contextInfo := extendedMsg.GetContextInfo()
		if contextInfo != nil {
			quotedMsg := contextInfo.GetQuotedMessage()
			if quotedMsg != nil {
				// Check quoted image or video/gif
				if quotedMsg.GetImageMessage() != nil || quotedMsg.GetVideoMessage() != nil {
					return true
				}
			}
		}
	}

	return false
}

func (bot *WhatsAppBot) hasQuotedSticker(msg *events.Message) bool {
	// Check direct sticker
	if msg.Message.GetStickerMessage() != nil {
		return true
	}

	// Check extended text message (replies)
	extendedMsg := msg.Message.GetExtendedTextMessage()
	if extendedMsg != nil {
		contextInfo := extendedMsg.GetContextInfo()
		if contextInfo != nil {
			quotedMsg := contextInfo.GetQuotedMessage()
			if quotedMsg != nil {
				// Check quoted sticker
				if quotedMsg.GetStickerMessage() != nil {
					return true
				}
			}
		}
	}

	return false
}

// sendReply - Send reply message with proper context info for group and DM
func (bot *WhatsAppBot) sendReply(chatJID types.JID, text string, quotedMsgID string, quotedSender types.JID) {
	fmt.Printf("📤 Sending reply: %s\n", text[:min(50, len(text))]+"...")

	isGroup := strings.Contains(chatJID.String(), "@g.us")

	// Create context info for reply
	contextInfo := &waProto.ContextInfo{
		StanzaID: proto.String(quotedMsgID),
	}

	// For group chats, add participant info
	if isGroup {
		contextInfo.Participant = proto.String(quotedSender.String())
	}

	msg := &waProto.Message{
		ExtendedTextMessage: &waProto.ExtendedTextMessage{
			Text:        proto.String(text),
			ContextInfo: contextInfo,
		},
	}

	_, err := bot.client.SendMessage(context.Background(), chatJID, msg)
	if err != nil {
		log.Printf("❌ Failed to send reply: %v", err)
	} else {
		fmt.Printf("✅ Reply sent successfully\n")
	}
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	fmt.Println("🤖 WhatsApp Bot - WebP Tools Edition")
	fmt.Println("⚡ fast response & WebP sticker support")
	fmt.Println("📱 support multiple users simultaneously")
	fmt.Println("🎯 proper WebP/PNG sticker handling")
	fmt.Println("=============================================")

	bot := NewWhatsAppBot()
	bot.Start()
}
