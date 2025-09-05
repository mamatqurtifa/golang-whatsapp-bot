// main.go - Updated version with proper reply functionality and calendar feature
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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
	httpClient        *http.Client
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
		httpClient:  &http.Client{Timeout: 10 * time.Second},
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

// extractQuotedMessageText - Extract text from quoted/replied message
func (bot *WhatsAppBot) extractQuotedMessageText(msg *events.Message) string {
	// Check extended text message for context info (replies)
	extendedMsg := msg.Message.GetExtendedTextMessage()
	if extendedMsg != nil {
		contextInfo := extendedMsg.GetContextInfo()
		if contextInfo != nil {
			quotedMsg := contextInfo.GetQuotedMessage()
			if quotedMsg != nil {
				// Try different quoted message types
				if quotedMsg.GetConversation() != "" {
					return quotedMsg.GetConversation()
				}
				if quotedMsg.GetExtendedTextMessage() != nil {
					return quotedMsg.GetExtendedTextMessage().GetText()
				}
				if quotedMsg.GetImageMessage() != nil {
					return quotedMsg.GetImageMessage().GetCaption()
				}
				if quotedMsg.GetVideoMessage() != nil {
					return quotedMsg.GetVideoMessage().GetCaption()
				}
			}
		}
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
		// Check if it's a group first
		if isGroup {
			// Try to get group info to get the actual group name
			groupInfo, err := bot.client.GetGroupInfo(chatJID)
			if err == nil {
				// Check if this is the "Slaviors Chat" group
				if strings.Contains(groupInfo.Name, "Slaviors Chat") {
					response = `haloo Slaviors members. aku bot dari mamat yang bakal nemenin grup ini biar ga sepi  ^_^`
					fmt.Printf("✅ Responding to /hi command in Slaviors Chat group\n")
				} else {
					// Default group response
					response = `halo grup! 👋

Commands:
/help - bantuan lengkap
/hi - sapa bot  
/sticker atau /s - gambar ke stiker (WebP)
/toimg - stiker ke gambar
/tagall - mention semua (grup only)
/calendar - tanggal hari ini WIB
/stats - statistik bot
/tools - cek WebP tools

bot siap melayani grup ini! 🤖`
					fmt.Printf("✅ Responding to /hi command (default group)\n")
				}
			} else {
				// Fallback if can't get group info - check using JID User (less reliable)
				if strings.Contains(chatJID.User, "Slaviors") || strings.Contains(strings.ToLower(chatJID.User), "slaviors") {
					response = `haloo Slaviors members. aku bot dari mamat yang bakal nemenin grup ini biar ga sepi  ^_^`
					fmt.Printf("✅ Responding to /hi command in Slaviors Chat group (fallback detection)\n")
				} else {
					// Default group response
					response = `halo grup! 👋

Commands:
/help - bantuan lengkap
/hi - sapa bot  
/sticker atau /s - gambar ke stiker (WebP)
/toimg - stiker ke gambar
/tagall - mention semua (grup only)
/calendar - tanggal hari ini WIB
/stats - statistik bot
/tools - cek WebP tools

bot siap melayani grup ini! 🤖`
					fmt.Printf("✅ Responding to /hi command (default group, fallback)\n")
				}
			}
		} else {
			// Default DM response
			response = `halo! 👋

Commands:
/help - bantuan lengkap
/hi - sapa bot  
/sticker atau /s - gambar ke stiker (WebP)
/toimg - stiker ke gambar
/calendar - tanggal hari ini WIB
/stats - statistik bot
/tools - cek WebP tools

bot siap melayani nih! 🤖`
			fmt.Printf("✅ Responding to /hi command (DM)\n")
		}

	case "/help":
		response = `🤖 WhatsApp Bot Helper - WebP Edition

aku bot yang bisa bantu convert sticker dengan WebP!

📋 Commands:
• /hi - menu utama
• /sticker atau /s - konversi gambar/gif ke stiker WebP 
• /toimg - konversi stiker ke gambar PNG
• /tagall - mention semua member (grup only)
• /calendar - info tanggal hari ini WIB
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
		response = bot.getCalendarInfo()
		fmt.Printf("✅ Responding to calendar command\n")

	case "/tagall":
		if isGroup {
			// Get the quoted message text for tagall
			quotedText := bot.extractQuotedMessageText(originalMsg)
			response = bot.TagAllHandler(chatJID, originalMsg.Info.ID, quotedText)
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

// getCalendarInfo - Get calendar information for WIB timezone with Hijri API
func (bot *WhatsAppBot) getCalendarInfo() string {
	// Set timezone to WIB (UTC+7)
	wib, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		// Fallback to manual UTC+7 offset
		wib = time.FixedZone("WIB", 7*60*60)
	}

	now := time.Now().In(wib)

	// Indonesian day names
	dayNames := []string{
		"Minggu", "Senin", "Selasa", "Rabu",
		"Kamis", "Jumat", "Sabtu",
	}

	// Indonesian month names
	monthNames := []string{
		"", "Januari", "Februari", "Maret", "April", "Mei", "Juni",
		"Juli", "Agustus", "September", "Oktober", "November", "Desember",
	}

	dayName := dayNames[now.Weekday()]
	monthName := monthNames[now.Month()]

	// Calculate week of year
	_, week := now.ISOWeek()

	// Calculate day of year
	dayOfYear := now.YearDay()

	// Get Hijri date from API
	hijriInfo := bot.getHijriDateFromAPI(now)

	response := fmt.Sprintf(`📅 *Kalender Hari Ini - WIB*

🗓️ *%s, %d %s %d*
🕐 *Pukul: %s WIB*

📊 *Detail:*
• Hari ke-%d dalam tahun %d
• Minggu ke-%d dalam tahun
• Kuartal ke-%d

🌙 *Tanggal Hijriyah:*
%s

⏰ *Zona Waktu:*
Waktu Indonesia Barat (WIB)
UTC +7

semoga harimu berkah ya! 🤲`,
		dayName, now.Day(), monthName, now.Year(),
		now.Format("15:04:05"),
		dayOfYear, now.Year(),
		week,
		((int(now.Month())-1)/3)+1,
		hijriInfo)

	return response
}

// getHijriDateFromAPI - Get accurate Hijri date from MyQuran API
func (bot *WhatsAppBot) getHijriDateFromAPI(date time.Time) string {
	fmt.Printf("🌙 Fetching Hijri date from MyQuran API...\n")

	// Format date as YYYY-MM-DD for API
	apiURL := "https://api.myquran.com/v2/cal/hijr"

	// Create HTTP request
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		fmt.Printf("❌ Failed to create request: %v\n", err)
		return bot.getFallbackHijriDate(date)
	}

	// Add query parameter for specific date (if API supports it)
	// For now, we'll use current date as API seems to return current date by default

	// Make request with timeout
	resp, err := bot.httpClient.Do(req)
	if err != nil {
		fmt.Printf("❌ API request failed: %v\n", err)
		return bot.getFallbackHijriDate(date)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != 200 {
		fmt.Printf("❌ API returned status: %d\n", resp.StatusCode)
		return bot.getFallbackHijriDate(date)
	}

	// Parse JSON response
	// Define HijriAPIResponse struct to match API response
	type HijriAPIResponse struct {
		Status bool `json:"status"`
		Data   struct {
			Date []string `json:"date"`
		} `json:"data"`
	}

	var apiResp HijriAPIResponse
	err = json.NewDecoder(resp.Body).Decode(&apiResp)
	if err != nil {
		fmt.Printf("❌ Failed to parse JSON: %v\n", err)
		return bot.getFallbackHijriDate(date)
	}

	// Check if API call was successful
	if !apiResp.Status {
		fmt.Printf("❌ API returned error status\n")
		return bot.getFallbackHijriDate(date)
	}

	// Extract Hijri information
	if len(apiResp.Data.Date) >= 2 {
		hijriDay := apiResp.Data.Date[0]  // e.g., "Jum'at"
		hijriDate := apiResp.Data.Date[1] // e.g., "12 Rabiul Awal 1447 H"

		fmt.Printf("✅ Hijri date fetched successfully: %s\n", hijriDate)
		return fmt.Sprintf("%s, %s", hijriDay, hijriDate)
	}

	// Fallback if data format is unexpected
	fmt.Printf("⚠️ Unexpected API response format, using fallback\n")
	return bot.getFallbackHijriDate(date)
}

// getFallbackHijriDate - Fallback Hijri date calculation if API fails
func (bot *WhatsAppBot) getFallbackHijriDate(gregorianDate time.Time) string {
	// Simple approximation - not astronomically accurate
	hijriEpoch := time.Date(622, 7, 16, 0, 0, 0, 0, time.UTC)
	daysSinceEpoch := gregorianDate.Sub(hijriEpoch).Hours() / 24

	// Approximate Hijri year (354.37 days per year)
	hijriYear := int(daysSinceEpoch/354.37) + 1

	// Approximate remaining days in current Hijri year
	remainingDays := int(daysSinceEpoch) % 354

	// Approximate month and day
	hijriMonth := 1
	hijriDay := remainingDays

	monthDays := []int{30, 29, 30, 29, 30, 29, 30, 29, 30, 29, 30, 29}

	for i, days := range monthDays {
		if hijriDay <= days {
			hijriMonth = i + 1
			break
		}
		hijriDay -= days
	}

	if hijriDay == 0 {
		hijriDay = 1
	}

	hijriMonthName := bot.getHijriMonthName(hijriMonth)

	return fmt.Sprintf("*%d %s %d H* (perkiraan - API gagal)", hijriDay, hijriMonthName, hijriYear)
}

// getHijriMonthName - Get Hijri month name in Indonesian
func (bot *WhatsAppBot) getHijriMonthName(month int) string {
	monthNames := []string{
		"", "Muharram", "Safar", "Rabiul Awal", "Rabiul Akhir",
		"Jumadil Awal", "Jumadil Akhir", "Rajab", "Syaban",
		"Ramadan", "Syawal", "Dzulkaidah", "Dzulhijjah",
	}

	if month >= 1 && month <= 12 {
		return monthNames[month]
	}
	return "Unknown"
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
	fmt.Println("📅 calendar info with WIB timezone")
	fmt.Println("=============================================")

	bot := NewWhatsAppBot()
	bot.Start()
}
