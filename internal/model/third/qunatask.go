package third

import (
	"time"
)

type QuNaTask struct {
	ID        uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	CityUrl   string     `gorm:"index;not null" json:"city_url"`
	City      string     `gorm:"type:varchar(255);not null;default:''" json:"city"`
	HotelId   string     `gorm:"index;not null" json:"hotel_id"`
	HotelName string     `gorm:"type:varchar(255);not null;default:''" json:"hotel_name"`
	Status    int        `gorm:"index;not null;default:0" json:"status"`
	BeginTime *time.Time `json:"begin_time"`
	EndTime   *time.Time `json:"end_time"`

	DeviceSerial string `gorm:"default:0" json:"device_serial"`
	//Device       model.Device `gorm:"-" json:"device,omitempty"`
	Result    string    `gorm:"type:text" json:"result"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (QuNaTask) TableName() string {
	return "qu_na_tasks"
}
