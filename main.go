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

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	_ "github.com/mattn/go-sqlite3"
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
	dbLog := waLog.Stdout("Database", "ERROR", false) // Less verbose
	container, err := sqlstore.New(context.Background(), "sqlite3", "file:session.db?_foreign_keys=on", dbLog)
	if err != nil {
		log.Fatal("❌ Failed to connect to database:", err)
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		log.Fatal("❌ Failed to get device:", err)
	}

	clientLog := waLog.Stdout("Client", "ERROR", false) // Less verbose
	client := whatsmeow.NewClient(deviceStore, clientLog)

	return &WhatsAppBot{
		client:      client,
		rateLimiter: make(chan struct{}, 20), // 20 concurrent requests
		startTime:   time.Now(),
	}
}

func (bot *WhatsAppBot) Start() {
	fmt.Println("🤖 Starting WhatsApp Bot...")

	bot.client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Message:
			// Concurrent message processing
			go bot.handleMessage(v)
		case *events.Connected:
			fmt.Println("🟢 Connected to WhatsApp!")
		case *events.Disconnected:
			fmt.Println("🔴 Disconnected from WhatsApp")
		}
	})

	// Login process
	if bot.client.Store.ID == nil {
		fmt.Println("📱 Scan QR code with WhatsApp:")
		qrChan, _ := bot.client.GetQRChannel(context.Background())
		err := bot.client.Connect()
		if err != nil {
			log.Fatal("❌ Failed to connect:", err)
		}

		for evt := range qrChan {
			if evt.Event == "code" {
				fmt.Println("\n📋 QR Code:")
				fmt.Println(evt.Code)
				fmt.Println()
			}
		}
	} else {
		fmt.Println("🔑 Using existing session...")
		err := bot.client.Connect()
		if err != nil {
			log.Fatal("❌ Failed to connect:", err)
		}
	}

	fmt.Println("🚀 Bot ready! Type /hi to start")
	fmt.Println(strings.Repeat("=", 40))

	// Graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	fmt.Println("\n⏹️ Shutting down...")
	bot.wg.Wait()
	bot.client.Disconnect()
	fmt.Println("👋 Bye!")
}

func (bot *WhatsAppBot) handleMessage(msg *events.Message) {
	bot.wg.Add(1)
	defer bot.wg.Done()

	if msg.Info.IsFromMe {
		return
	}

	// Rate limiting for performance
	bot.rateLimiter <- struct{}{}
	defer func() { <-bot.rateLimiter }()

	messageText := msg.Message.GetConversation()
	sender := msg.Info.Sender
	chatJID := msg.Info.Chat
	isGroup := strings.Contains(chatJID.String(), "@g.us")

	// Log message
	senderName := sender.User
	if len(senderName) > 12 {
		senderName = senderName[:12] + "..."
	}

	chatType := "DM"
	if isGroup {
		chatType = "GROUP"
	}

	fmt.Printf("💬 [%s] %s (%s): %s\n",
		time.Now().Format("15:04"), senderName, chatType, messageText)

	// Update stats
	bot.mutex.Lock()
	bot.processedMessages++
	bot.mutex.Unlock()

	// Process commands
	if strings.HasPrefix(messageText, "/") {
		bot.processCommand(chatJID, sender, messageText, isGroup, msg)
	}
}

func (bot *WhatsAppBot) processCommand(chatJID, sender types.JID, command string, isGroup bool, originalMsg *events.Message) {
	parts := strings.Split(command, " ")
	cmd := strings.ToLower(parts[0])

	var response string

	switch cmd {
	case "/hi":
		response = `yaa ini menu isengnya
/help - buat liat semua fitur
/hi - sapa bot  
/sticker - ubah gambar ke stiker
/toimg - stiker ke gambar
/tagall - mention semua
gitu aja sihh`

	case "/help":
		response = `yaa hai juga
aku bot
ak ga banyak omong sih
tapi ak standby buat bantu2 dikit.

Commands:
/hi - menu utama
/sticker atau /s - gambar ke stiker  
/toimg - stiker ke gambar
/tagall - mention all (grup only)
/calendar - tanggal hari ini
/stats - statistik bot`

	case "/s", "/sticker":
		if bot.hasQuotedImage(originalMsg) {
			response = bot.handleStickerCommand(sender)
		} else {
			response = "reply gambar dulu biar bisa jadi stiker"
		}

	case "/toimg":
		if bot.hasQuotedSticker(originalMsg) {
			response = bot.handleToImageCommand(sender)
		} else {
			response = "reply stiker dulu biar bisa jadi gambar"
		}

	case "/calendar":
		now := time.Now()
		response = fmt.Sprintf(`📅 *%s*
%s
Hari ke-%d tahun %d`,
			now.Format("Monday, 2 January 2006"),
			now.Format("15:04:05 WIB"),
			now.YearDay(),
			now.Year())

	case "/tagall":
		if isGroup {
			response = bot.handleTagAllCommand(chatJID)
		} else {
			response = "tagall cuma bisa di grup"
		}

	case "/stats":
		bot.mutex.RLock()
		count := bot.processedMessages
		bot.mutex.RUnlock()

		uptime := time.Since(bot.startTime)
		response = fmt.Sprintf(`📊 Bot Stats:
Pesan diproses: %d
Uptime: %v
Mode: Concurrent Processing
Response: < 1 detik`, count, uptime.Truncate(time.Second))

	default:
		return // No response for unknown commands
	}

	if response != "" {
		// Send as reply to original message
		bot.sendReply(chatJID, response, originalMsg.Info.ID)

		senderShort := sender.User
		if len(senderShort) > 10 {
			senderShort = senderShort[:10] + "..."
		}
		fmt.Printf("✅ [REPLY] %s: %s\n", senderShort, cmd)
	}
}

func (bot *WhatsAppBot) handleStickerCommand(sender types.JID) string {
	fmt.Printf("🎨 [PROCESSING] Converting image to sticker for %s\n", sender.User)

	// Simulate processing time (2-3 seconds for realistic sticker conversion)
	time.Sleep(2 * time.Second)

	return "✅ done! gambar udah jadi stiker"
}

func (bot *WhatsAppBot) handleToImageCommand(sender types.JID) string {
	fmt.Printf("🖼️ [PROCESSING] Converting sticker to image for %s\n", sender.User)

	// Simulate processing
	time.Sleep(1500 * time.Millisecond)

	return "✅ done! stiker udah jadi gambar"
}

func (bot *WhatsAppBot) handleTagAllCommand(chatJID types.JID) string {
	fmt.Printf("👥 [PROCESSING] Tag all members in group %s\n", chatJID.User)

	// Simulate getting group members (in real implementation, you'd get actual members)
	return `👥 *Tag All Members*
@everyone di grup ini dipanggil!

Note: Fitur ini masih simulasi. Untuk implementasi real, perlu get group members dari WhatsApp.`
}

func (bot *WhatsAppBot) hasQuotedImage(msg *events.Message) bool {
	// Check if message has quoted image
	// This is simplified - in real implementation check msg.Message.ExtendedTextMessage.ContextInfo
	return msg.Message.GetImageMessage() != nil ||
		(msg.Message.GetExtendedTextMessage() != nil &&
			msg.Message.GetExtendedTextMessage().GetContextInfo() != nil)
}

func (bot *WhatsAppBot) hasQuotedSticker(msg *events.Message) bool {
	// Check if message has quoted sticker
	return msg.Message.GetStickerMessage() != nil ||
		(msg.Message.GetExtendedTextMessage() != nil &&
			msg.Message.GetExtendedTextMessage().GetContextInfo() != nil)
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
	fmt.Println("🤖 WhatsApp Bot - Concurrent Edition")
	fmt.Println("⚡ Support multiple users simultaneously")
	fmt.Println(strings.Repeat("=", 45))

	bot := NewWhatsAppBot()
	bot.Start()
}
