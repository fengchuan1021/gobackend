package model

// DeviceUserProfile 设备上的 Android 多用户 profile
type DeviceUserProfile struct {
	ID           uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	DeviceSerial string `gorm:"type:varchar(64);uniqueIndex:idx_device_user;not null" json:"device_serial"`
	UserID       uint   `gorm:"uniqueIndex:idx_device_user;not null" json:"user_id"`
	Name         string `gorm:"type:varchar(128)" json:"name"`
}

// TableName 指定表名
func (DeviceUserProfile) TableName() string {
	return "DeviceUserProfile"
}
