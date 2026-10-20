package title

// Default is a seeded config_titles row (migrations/027 and admin SeedTitles write the same).
// The milestone granting each one is milestone.TitleRewards.
type Default struct {
	ID          string
	Name        string
	Rarity      string
	SortOrder   int
	Description string
}

var Defaults = []Default{
	{"veteran", "Chiến binh kỳ cựu", RarityBlue, 10, "1 nhân vật đạt cấp 40"},
	{"smith", "Thợ rèn lành nghề", RarityBlue, 20, "1 trang bị đạt +10"},
	{"sharpshooter", "Xạ thủ bách chiến", RarityOrange, 30, "Thắng 100 trận"},
	{"old_hand", "Lão làng", RarityOrange, 40, "Chơi 1000 trận"},
	{"elite_armor", "Chiến giáp tinh anh", RarityPurple, 50, "1 bộ trang bị hoàn chỉnh +14"},
	{"battle_master", "Bậc thầy chiến trường", RarityPurple, 60, "1 nhân vật đạt cấp 80"},
	{"legend_smith", "Thợ rèn huyền thoại", RarityRed, 70, "1 trang bị đạt +16"},
	{"grandmaster", "Cao thủ", RarityRed, 80, "Lần đầu đạt hạng Cao thủ"},
}
