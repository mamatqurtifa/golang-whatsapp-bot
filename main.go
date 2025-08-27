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
    "go.mau.fi/whatsmeow/store/sqlstore"
    "go.mau.fi/whatsmeow/types"
    "go.mau.fi/whatsmeow/types/events"
    waLog "go.mau.fi/whatsmeow/util/log"
    waProto "go.mau.fi/whatsmeow/binary/proto"
    "google.golang.org/protobuf/proto"

    _ "github.com/mattn/go-sqlite3"
)

type WhatsAppBot struct {
    client *whatsmeow.Client
    rateLimiter chan struct{}
    wg sync.WaitGroup
    mutex sync.RWMutex
    processedMessages int64
}

func NewWhatsAppBot() *WhatsAppBot {
    // Setup database untuk session
    dbLog := waLog.Stdout("Database", "INFO", true)
    container, err := sqlstore.New("sqlite3", "file:session.db?_foreign_keys=on", dbLog)
    if err != nil {
        log.Fatal("❌ Failed to connect to database:", err)
    }

    deviceStore, err := container.GetFirstDevice()
    if err != nil {
        log.Fatal("❌ Failed to get device:", err)
    }

    clientLog := waLog.Stdout("Client", "INFO", true)
    client := whatsmeow.NewClient(deviceStore, clientLog)

    bot := &WhatsAppBot{
        client: client,
        // Rate limiter: maksimal 15 pesan bersamaan
        rateLimiter: make(chan struct{}, 15),
    }

    return bot
}

func (bot *WhatsAppBot) Start() {
    fmt.Println("🤖 Starting WhatsApp Bot...")
    
    // Event handler
    bot.client.AddEventHandler(func(evt interface{}) {
        switch v := evt.(type) {
        case *events.Message:
            // Setiap pesan diproses concurrent
            go bot.handleMessage(v)
        case *events.Receipt:
            fmt.Printf("✅ Message delivered: %v\n", v.MessageIDs)
        case *events.Connected:
            fmt.Println("🟢 Connected to WhatsApp!")
        case *events.Disconnected:
            fmt.Println("🔴 Disconnected from WhatsApp")
        }
    })

    // Login process
    if bot.client.Store.ID == nil {
        // First time login
        fmt.Println("📱 First time login - Please scan QR code with WhatsApp:")
        qrChan, _ := bot.client.GetQRChannel(context.Background())
        err := bot.client.Connect()
        if err != nil {
            log.Fatal("❌ Failed to connect:", err)
        }

        for evt := range qrChan {
            if evt.Event == "code" {
                fmt.Println("\n📋 QR Code (scan with WhatsApp):")
                fmt.Println(evt.Code)
                fmt.Println("\nOr you can use QR code scanner app to scan this text")
            } else {
                fmt.Println("📱 Login event:", evt.Event)
            }
        }
    } else {
        // Already have session
        fmt.Println("🔑 Using existing session...")
        err := bot.client.Connect()
        if err != nil {
            log.Fatal("❌ Failed to connect:", err)
        }
    }

    fmt.Println("🚀 Bot is now running! Send /help to get started")
    fmt.Println("📊 Bot supports concurrent message processing")

    // Graceful shutdown
    c := make(chan os.Signal, 1)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    <-c

    fmt.Println("\n⏹️ Shutting down bot...")
    bot.wg.Wait() // Wait for all goroutines to finish
    bot.client.Disconnect()
    fmt.Println("👋 Bot stopped!")
}

func (bot *WhatsAppBot) handleMessage(msg *events.Message) {
    // Concurrent processing setup
    bot.wg.Add(1)
    defer bot.wg.Done()

    // Skip own messages
    if msg.Info.IsFromMe {
        return
    }

    // Rate limiting
    bot.rateLimiter <- struct{}{}
    defer func() { <-bot.rateLimiter }()

    messageText := msg.Message.GetConversation()
    sender := msg.Info.Sender
    chatJID := msg.Info.Chat

    // Log incoming message
    senderName := sender.User
    if len(senderName) > 10 {
        senderName = senderName[:10] + "..."
    }
    
    fmt.Printf("💬 [%s] %s: %s\n", time.Now().Format("15:04:05"), senderName, messageText)

    // Update stats (thread-safe)
    bot.mutex.Lock()
    bot.processedMessages++
    currentCount := bot.processedMessages
    bot.mutex.Unlock()

    // Process commands
    if strings.HasPrefix(messageText, "/") {
        fmt.Printf("⚡ [CONCURRENT] Processing command from %s\n", senderName)
        bot.processCommand(chatJID, sender, messageText)
    }
}

func (bot *WhatsAppBot) processCommand(chatJID, sender types.JID, command string) {
    parts := strings.Split(command, " ")
    cmd := strings.ToLower(parts[0])

    var response string
    var needsProcessing bool = true

    switch cmd {
    case "/s", "/sticker":
        response = bot.handleStickerCommand(chatJID, sender)
    
    case "/ping":
        response = "🏓 Pong! Bot is working perfectly!"
    
    case "/time":
        response = fmt.Sprintf("⏰ Current time: %s", time.Now().Format("15:04:05 - 02/01/2006"))
    
    case "/help":
        response = `🤖 *WhatsApp Bot Commands:*

📌 *Basic Commands:*
• /ping - Test bot response
• /time - Get current time
• /stats - Bot statistics
• /help - Show this help

🎨 *Media Commands:*
• /s or /sticker - Convert image to sticker
• /quote - Generate quote image

📊 *Info Commands:*
• /info - Bot information
• /uptime - How long bot running

✨ *Features:*
• Concurrent processing (handle multiple users at once)
• Fast response time
• No queue system`

    case "/stats":
        bot.mutex.RLock()
        count := bot.processedMessages
        bot.mutex.RUnlock()
        
        uptime := time.Since(time.Now().Add(-time.Hour)) // Placeholder
        response = fmt.Sprintf(`📊 *Bot Statistics:*
• Messages processed: %d
• Active goroutines: Multiple
• Processing mode: Concurrent
• Response time: < 1s`, count)

    case "/info":
        response = `ℹ️ *Bot Information:*
• Name: WhatsApp Concurrent Bot
• Version: 1.0.0
• Language: Go (Golang)
• Library: whatsmeow
• Mode: Concurrent processing
• Developer: Custom Bot`

    case "/quote":
        response = bot.handleQuoteCommand(parts)
    
    default:
        needsProcessing = false
    }

    if needsProcessing {
        // Send response
        bot.sendMessage(chatJID, response)
        
        senderName := sender.User
        if len(senderName) > 10 {
            senderName = senderName[:10] + "..."
        }
        fmt.Printf("✅ [SENT] Response to %s: %s\n", senderName, cmd)
    }
}

func (bot *WhatsAppBot) handleStickerCommand(chatJID, sender types.JID) string {
    fmt.Printf("🎨 [PROCESSING] Creating sticker for %s...\n", sender.User)
    
    // Simulate sticker processing (replace with real implementation)
    time.Sleep(2 * time.Second)
    
    return `🎨 *Sticker Command*

To create a sticker:
1. Send an image
2. Reply to the image with /s or /sticker
3. Bot will convert it to sticker

⚡ *Note:* This bot processes multiple requests simultaneously!`
}

func (bot *WhatsAppBot) handleQuoteCommand(parts []string) string {
    if len(parts) < 2 {
        return "💬 Usage: /quote your text here"
    }
    
    quoteText := strings.Join(parts[1:], " ")
    return fmt.Sprintf(`💬 *Quote Generated:*

"%s"

- WhatsApp Bot User`, quoteText)
}

func (bot *WhatsAppBot) sendMessage(chatJID types.JID, text string) {
    msg := &waProto.Message{
        Conversation: proto.String(text),
    }

    _, err := bot.client.SendMessage(context.Background(), chatJID, msg)
    if err != nil {
        log.Printf("❌ Failed to send message: %v", err)
    }
}

func main() {
    fmt.Println("🤖 WhatsApp Concurrent Bot Starting...")
    fmt.Println("📝 Made with Go (Golang) + whatsmeow")
    fmt.Println("⚡ Supports concurrent message processing")
    fmt.Println("=" * 50)
    
    bot := NewWhatsAppBot()
    bot.Start()
}