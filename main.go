// main.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
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
	fmt.Println("Starting WhatsApp Bot...")

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

	messageText := msg.Message.GetConversation()
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

	// Enhanced message logging
	timestamp := time.Now().Format("15:04:05")
	fmt.Printf("\n📨 [%s] %s%s\n", timestamp, chatType, chatInfo)
	fmt.Printf("👤 From: +%s\n", sender.User)
	fmt.Printf("💬 Message: %s\n", messageText)

	bot.mutex.Lock()
	bot.processedMessages++
	currentCount := bot.processedMessages
	bot.mutex.Unlock()

	fmt.Printf("📊 Total processed: %d\n", currentCount)

	if strings.HasPrefix(messageText, "/") {
		fmt.Printf("⚡ Processing command: %s\n", strings.Split(messageText, " ")[0])
		bot.processCommand(chatJID, sender, messageText, isGroup, msg)
	}

	fmt.Println("----------------------------------------")
}

func (bot *WhatsAppBot) processCommand(chatJID, sender types.JID, command string, isGroup bool, originalMsg *events.Message) {
	parts := strings.Split(command, " ")
	cmd := strings.ToLower(parts[0])
	var response string

	startTime := time.Now()

	switch cmd {
	case "/hi":
		response = `👋 Halo!

Commands:
/help - Bantuan lengkap
/hi - Sapa bot  
/sticker atau /s - Gambar ke stiker
/toimg - Stiker ke gambar
/tagall - Mention semua (grup only)
/calendar - Tanggal hari ini
/stats - Statistik bot

Bot siap melayani! 🤖`

	case "/help":
		response = `🤖 WhatsApp Bot Helper

Aku bot sederhana yang bisa bantu beberapa hal:

📋 Commands:
• /hi - Menu utama
• /sticker atau /s - Konversi gambar/gif ke stiker  
• /toimg - Konversi stiker ke gambar
• /tagall - Mention semua member (grup only)
• /calendar - Info tanggal hari ini
• /stats - Statistik bot

⚡ Response time: < 500ms
🔄 Concurrent processing: Ya
💪 24/7 Ready!`

	case "/s", "/sticker":
		if bot.hasQuotedImage(originalMsg) {
			response = bot.StickerHandler(sender) // Fixed: was handleStickerCommand
		} else {
			response = "❌ Reply gambar atau gif dulu untuk dijadikan stiker"
		}

	case "/toimg":
		if bot.hasQuotedSticker(originalMsg) {
			response = bot.ToImageHandler(sender) // Fixed: was handleToImageCommand
		} else {
			response = "❌ Reply stiker dulu untuk dikonversi ke gambar"
		}

	case "/calendar":
		now := time.Now()
		response = fmt.Sprintf(`📅 **%s**
🕐 %s WIB
📊 Hari ke-%d tahun %d
🗓️ Minggu ke-%d

Semoga harimu menyenangkan! 😊`,
			now.Format("Monday, 2 January 2006"),
			now.Format("15:04:05"),
			now.YearDay(),
			now.Year(),
			getWeekOfYear(now))

	case "/tagall":
		if isGroup {
			response = bot.TagAllHandler(chatJID) // Fixed: was handleTagAllCommand
		} else {
			response = "❌ Command /tagall hanya bisa digunakan di grup"
		}

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

		response = fmt.Sprintf(`📊 **Bot Statistics**

💬 Pesan diproses: **%d**
⏱️ Uptime: **%v**
📈 Rata-rata: **%.1f** msg/menit
⚡ Mode: Concurrent Processing
🚀 Response time: < 500ms
📱 Status: Online & Ready

Keep chatting! 🤖✨`,
			count,
			uptime.Truncate(time.Second),
			msgPerMin)

	default:
		return // No response for unknown commands
	}

	if response != "" {
		// Send reply immediately
		go func() {
			bot.sendReply(chatJID, response, originalMsg.Info.ID)

			processingTime := time.Since(startTime)
			senderShort := sender.User
			if len(senderShort) > 10 {
				senderShort = senderShort[:10] + "..."
			}
			fmt.Printf("✅ REPLIED to %s: %s (took %v)\n", senderShort, cmd, processingTime)
		}()
	}
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

func (bot *WhatsAppBot) sendReply(chatJID types.JID, text string, quotedMsgID string) {
	msg := &waProto.Message{
		ExtendedTextMessage: &waProto.ExtendedTextMessage{
			Text: proto.String(text),
			ContextInfo: &waProto.ContextInfo{
				StanzaID: proto.String(quotedMsgID),
			},
		},
	}

	_, err := bot.client.SendMessage(context.Background(), chatJID, msg)
	if err != nil {
		log.Printf("❌ Failed to send reply: %v", err)
	}
}

func main() {
	fmt.Println("🤖 WhatsApp Bot - Enhanced Edition")
	fmt.Println("⚡ Fast response & concurrent processing")
	fmt.Println("📱 Support multiple users simultaneously")
	fmt.Println("=============================================")

	bot := NewWhatsAppBot()
	bot.Start()
}
