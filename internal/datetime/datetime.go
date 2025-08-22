package datetime

import (
    "time"

    "google.golang.org/protobuf/types/known/timestamppb"
)

// MakeExpiration returns a Timestamp set to midnight UTC 'days' days
// after today.  By flooring to UTC midnight, two servers on the same
// date (regardless of wall-clock moment or local time zone) will
// get identically the same Timestamp.
func MakeExpiration(days int) *timestamppb.Timestamp {
    // 1) Grab now in UTC
    now := time.Now().UTC()

    // 2) Floor to UTC midnight of today
    today := time.Date(
        now.Year(), now.Month(), now.Day(),
        0, 0, 0, 0,
        time.UTC,
    )

    // 3) Add the requested number of whole days
    expiry := today.Add(time.Duration(days) * 24 * time.Hour)

    // 4) Wrap in a protobuf Timestamp
    return timestamppb.New(expiry)
}

func GetTimestamp() string {
    // for now only return YYYY-MM-DD
    return time.Now().UTC().Format(time.DateOnly)
}