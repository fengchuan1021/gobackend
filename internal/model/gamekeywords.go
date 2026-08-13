package model

// GameKeywords 游戏关键词模型
type GameKeywords struct {
	ID           uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	Keyword      string `gorm:"type:varchar(255);not null;uniqueIndex" json:"keyword"`
	CategoryName string `gorm:"type:varchar(255);not null" json:"category_name"`
}

// TableName 指定表名
func (GameKeywords) TableName() string {
	return "game_keywords"
}
