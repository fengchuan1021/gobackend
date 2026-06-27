package model

import "time"

type QuestionAnswer struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Question    string    `gorm:"type:varchar(512);not null;uniqueIndex" json:"question"`
	Answers     []string  `gorm:"serializer:json" json:"answers"`
	RightAnswer string    `gorm:"type:varchar(255);not null" json:"right_answer"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (QuestionAnswer) TableName() string {
	return "question_answer"
}
