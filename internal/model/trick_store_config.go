package model

// TrickStoreConfig 对应表 tricky_store_config
type TrickStoreConfig struct {
	ID      uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	XML     string `gorm:"column:xml;type:text" json:"xml"`
	Datestr string `gorm:"column:datestr;type:varchar(255);default:''" json:"datestr"`
	Target  string `gorm:"column:target;type:text" json:"target"`
	Model   string `gorm:"column:model;type:varchar(255);default:''" json:"model"`
}

// TableName 指定表名
func (TrickStoreConfig) TableName() string {
	return "tricky_store_config"
}
