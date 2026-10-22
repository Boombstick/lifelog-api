package model

type ModerationStatus string

const (
	ModerationStatusUnknown  ModerationStatus = "unknown"
	ModerationStatusPending  ModerationStatus = "pending"
	ModerationStatusApproved ModerationStatus = "approved"
	ModerationStatusRejected ModerationStatus = "rejected"
)
