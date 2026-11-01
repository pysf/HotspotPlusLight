package domain

// SessionTarget identifies an open router session for a Disconnect-Request (BR-18).
type SessionTarget struct {
	NASIP, Username, AcctSessionID, FramedIP, MAC string
}
