package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
)

type config struct {
	token           string
	window          time.Duration
	threshold       int
	exemptRoles     map[string]struct{}
	logChannelID    string
	action          string // "ban" or "timeout"
	timeoutDuration time.Duration
	banDeleteDays   int // Ban時に遡って削除するメッセージの日数 (0-7)
}

func loadConfig() (*config, error) {
	cfg := &config{
		token:           os.Getenv("DISCORD_TOKEN"),
		window:          time.Minute,
		threshold:       3,
		exemptRoles:     make(map[string]struct{}),
		logChannelID:    os.Getenv("LOG_CHANNEL_ID"),
		action:          "ban",
		timeoutDuration: 24 * time.Hour,
		banDeleteDays:   1,
	}
	if cfg.token == "" {
		return nil, fmt.Errorf("DISCORD_TOKEN が未設定です")
	}
	if v := os.Getenv("SPAM_WINDOW_SECONDS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("SPAM_WINDOW_SECONDS が不正です: %q", v)
		}
		cfg.window = time.Duration(n) * time.Second
	}
	if v := os.Getenv("SPAM_CHANNEL_THRESHOLD"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 2 {
			return nil, fmt.Errorf("SPAM_CHANNEL_THRESHOLD が不正です (2以上): %q", v)
		}
		cfg.threshold = n
	}
	for _, id := range strings.Split(os.Getenv("EXEMPT_ROLE_IDS"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			cfg.exemptRoles[id] = struct{}{}
		}
	}
	if v := os.Getenv("ACTION"); v != "" {
		if v != "ban" && v != "timeout" {
			return nil, fmt.Errorf("ACTION は ban か timeout: %q", v)
		}
		cfg.action = v
	}
	if v := os.Getenv("TIMEOUT_MINUTES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("TIMEOUT_MINUTES が不正です: %q", v)
		}
		cfg.timeoutDuration = time.Duration(n) * time.Minute
	}
	return cfg, nil
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}

	dg, err := discordgo.New("Bot " + cfg.token)
	if err != nil {
		log.Fatal(err)
	}
	dg.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentMessageContent

	// メモリ節約: ロール判定に必要なギルド情報以外はキャッシュしない
	dg.State.TrackMembers = false
	dg.State.TrackThreadMembers = false
	dg.State.TrackPresences = false
	dg.State.TrackVoice = false
	dg.State.TrackEmojis = false
	dg.State.MaxMessageCount = 0

	tracker := newTracker(cfg.window, cfg.threshold)
	tracker.startSweeper(cfg.window)

	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		handleMessage(s, cfg, tracker, m)
	})

	if err := dg.Open(); err != nil {
		log.Fatal(err)
	}
	defer dg.Close()

	log.Printf("起動しました: window=%s threshold=%d action=%s", cfg.window, cfg.threshold, cfg.action)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("終了します")
}

func handleMessage(s *discordgo.Session, cfg *config, tracker *Tracker, m *discordgo.MessageCreate) {
	if m.GuildID == "" || m.Author == nil || m.Author.Bot || len(m.Attachments) == 0 {
		return
	}
	if isExempt(s, cfg, m) {
		return
	}

	now := time.Now()
	for _, att := range m.Attachments {
		imageKey := fmt.Sprintf("%s:%d", att.Filename, att.Size)
		refs, act := tracker.Record(m.Author.ID, imageKey, m.ChannelID, m.ID, now)
		if act {
			punish(s, cfg, m.GuildID, m.Author, att.Filename, refs)
			return
		}
	}
}

func isExempt(s *discordgo.Session, cfg *config, m *discordgo.MessageCreate) bool {
	if m.Member != nil {
		for _, roleID := range m.Member.Roles {
			if _, ok := cfg.exemptRoles[roleID]; ok {
				return true
			}
		}
	}
	g, err := s.State.Guild(m.GuildID)
	if err != nil {
		return false
	}
	if g.OwnerID == m.Author.ID {
		return true
	}
	if m.Member != nil {
		for _, role := range g.Roles {
			if role.Permissions&discordgo.PermissionAdministrator != 0 &&
				slices.Contains(m.Member.Roles, role.ID) {
				return true
			}
		}
	}
	return false
}

func punish(s *discordgo.Session, cfg *config, guildID string, user *discordgo.User, filename string, refs []msgRef) {
	channels := make(map[string]struct{})
	for _, r := range refs {
		channels[r.ChannelID] = struct{}{}
	}
	reason := fmt.Sprintf("画像スパム検出: %q を %d チャンネルに %s 以内に投稿",
		filename, len(channels), cfg.window)

	var actErr error
	switch cfg.action {
	case "timeout":
		until := time.Now().Add(cfg.timeoutDuration)
		actErr = s.GuildMemberTimeout(guildID, user.ID, &until)
		// タイムアウトはメッセージが残るので個別に削除する
		for _, r := range refs {
			if err := s.ChannelMessageDelete(r.ChannelID, r.MessageID); err != nil {
				log.Printf("メッセージ削除失敗 %s/%s: %v", r.ChannelID, r.MessageID, err)
			}
		}
	default:
		// Banは delete days 指定で直近メッセージも一括削除される
		actErr = s.GuildBanCreateWithReason(guildID, user.ID, reason, cfg.banDeleteDays)
	}

	if actErr != nil {
		log.Printf("%s 失敗 user=%s (%s): %v", cfg.action, user.Username, user.ID, actErr)
		notify(s, cfg, fmt.Sprintf("⚠️ スパム検出したが %s に失敗: <@%s> (`%s`)\n理由: %s\nエラー: %v",
			cfg.action, user.ID, user.ID, reason, actErr))
		return
	}

	log.Printf("%s 実行 user=%s (%s): %s", cfg.action, user.Username, user.ID, reason)
	notify(s, cfg, fmt.Sprintf("🔨 %s 実行: <@%s> (`%s`)\n%s", cfg.action, user.ID, user.ID, reason))
}

func notify(s *discordgo.Session, cfg *config, msg string) {
	if cfg.logChannelID == "" {
		return
	}
	if _, err := s.ChannelMessageSend(cfg.logChannelID, msg); err != nil {
		log.Printf("ログチャンネルへの通知失敗: %v", err)
	}
}
