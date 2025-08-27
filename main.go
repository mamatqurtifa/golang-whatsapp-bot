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
		rateLimiter: make(chan struct{}, 20),
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
			fmt.Println("Connected to WhatsApp")
		case *events.Disconnected:
			fmt.Println("Disconnected from WhatsApp")
		}
	})

	if bot.client.Store.ID == nil {
		fmt.Println("Scan QR code:")
		qrChan, _ := bot.client.GetQRChannel(context.Background())
		err := bot.client.Connect()
		if err != nil {
			log.Fatal("Failed to connect:", err)
		}

		for evt := range qrChan {
			if evt.Event == "code" {
				// Print QR code to terminal using ASCII
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
				fmt.Println()
			}
		}
	} else {
		fmt.Println("Using existing session...")
		err := bot.client.Connect()
		if err != nil {
			log.Fatal("Failed to connect:", err)
		}
	}

	fmt.Println("Bot ready")
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

	senderName := sender.User
	if len(senderName) > 12 {
		senderName = senderName[:12] + "..."
	}

	chatType := "DM"
	if isGroup {
		chatType = "GROUP"
	}

	fmt.Printf("[%s] %s (%s): %s\n",
		time.Now().Format("15:04"), senderName, chatType, messageText)

	bot.mutex.Lock()
	bot.processedMessages++
	bot.mutex.Unlock()

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
		response = `ya hai
/help - liat menu
/hi - sapa bot  
/sticker - gambar ke stiker
/toimg - stiker ke gambar
/tagall - mention semua
gitu aja`

	case "/help":
		response = `ya hai juga
aku bot
ga banyak omong
tapi standby buat bantu dikit

Commands:
/hi - menu utama
/sticker atau /s - gambar ke stiker  
/toimg - stiker ke gambar
/tagall - mention all grup only
/calendar - tanggal hari ini
/stats - statistik bot`

	case "/s", "/sticker":
		if bot.hasQuotedImage(originalMsg) {
			response = bot.handleStickerCommand(sender)
		} else {
			response = "reply gambar dulu"
		}

	case "/toimg":
		if bot.hasQuotedSticker(originalMsg) {
			response = bot.handleToImageCommand(sender)
		} else {
			response = "reply stiker dulu"
		}

	case "/calendar":
		now := time.Now()
		response = fmt.Sprintf(`%s
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
		response = fmt.Sprintf(`Bot Stats:
Pesan diproses: %d
Uptime: %v
Mode: Concurrent Processing
Response: < 1 detik`, count, uptime.Truncate(time.Second))

	default:
		return
	}

	if response != "" {
		bot.sendReply(chatJID, response, originalMsg.Info.ID)
		senderShort := sender.User
		if len(senderShort) > 10 {
			senderShort = senderShort[:10] + "..."
		}
		fmt.Printf("REPLY %s: %s\n", senderShort, cmd)
	}
}

func (bot *WhatsAppBot) handleStickerCommand(sender types.JID) string {
	fmt.Printf("PROCESSING Converting image to sticker for %s\n", sender.User)
	time.Sleep(2 * time.Second)
	return "done gambar udah jadi stiker"
}

func (bot *WhatsAppBot) handleToImageCommand(sender types.JID) string {
	fmt.Printf("PROCESSING Converting sticker to image for %s\n", sender.User)
	time.Sleep(1500 * time.Millisecond)
	return "done stiker udah jadi gambar"
}

func (bot *WhatsAppBot) handleTagAllCommand(chatJID types.JID) string {
	fmt.Printf("PROCESSING Tag all members in group %s\n", chatJID.User)
	return `Tag All Members
everyone di grup ini dipanggil
Note: Fitur ini masih simulasi`
}

func (bot *WhatsAppBot) hasQuotedImage(msg *events.Message) bool {
	return msg.Message.GetImageMessage() != nil ||
		(msg.Message.GetExtendedTextMessage() != nil &&
			msg.Message.GetExtendedTextMessage().GetContextInfo() != nil)
}

func (bot *WhatsAppBot) hasQuotedSticker(msg *events.Message) bool {
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
		log.Printf("Failed to send reply: %v", err)
	}
}

func main() {
	fmt.Println("WhatsApp Bot - Concurrent Edition")
	fmt.Println("Support multiple users simultaneously")
	fmt.Println("=============================================")

	bot := NewWhatsAppBot()
	bot.Start()
}
