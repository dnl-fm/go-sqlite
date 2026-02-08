package main

import (
	"fmt"
	"log"
	"time"

	"github.com/dnl-fm/go-sqlite/pkg/zeit"
)

func main() {
	fmt.Println("=== Zeit Timezone Examples ===")
	fmt.Println()

	// Example 1: Current time in different timezones
	fmt.Println("1. Current Time in Different Timezones")
	utc := time.UTC
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	newyork, _ := time.LoadLocation("America/New_York")

	nowUTC := zeit.Now(utc)
	nowTokyo := zeit.Now(tokyo)
	nowNewYork := zeit.Now(newyork)

	fmt.Printf("   UTC:      %s\n", nowUTC.ToUser())
	fmt.Printf("   Tokyo:    %s\n", nowTokyo.ToUser())
	fmt.Printf("   New York: %s\n", nowNewYork.ToUser())

	// Example 2: Creating Zeit from specific date
	fmt.Println()
	fmt.Println("2. Creating Zeit from Specific Date")
	t := time.Date(2024, 1, 15, 10, 30, 0, 0, utc)
	z := zeit.New(t, utc)
	fmt.Printf("   Created: %s\n", z.ToUser())
	fmt.Printf("   Unix timestamp: %d\n", z.Unix())

	// Example 3: Database serialization
	fmt.Println()
	fmt.Println("3. Database Serialization")
	dbTimestamp := z.ToDatabase()
	fmt.Printf("   Database timestamp: %d\n", dbTimestamp)
	restored := zeit.FromDatabase(dbTimestamp, utc)
	fmt.Printf("   Restored: %s\n", restored.ToUser())
	fmt.Printf("   Match: %v\n", z.Equal(restored))

	// Example 4: Date arithmetic
	fmt.Println()
	fmt.Println("4. Date Arithmetic")
	today := zeit.Now(utc)
	tomorrow := today.AddDays(1)
	nextWeek := today.AddDays(7)
	nextMonth := today.AddDays(30)

	fmt.Printf("   Today:      %s\n", today.Format("2006-01-02"))
	fmt.Printf("   Tomorrow:   %s\n", tomorrow.Format("2006-01-02"))
	fmt.Printf("   Next week:  %s\n", nextWeek.Format("2006-01-02"))
	fmt.Printf("   Next month: %s\n", nextMonth.Format("2006-01-02"))

	// Example 5: Business day arithmetic
	fmt.Println()
	fmt.Println("5. Business Day Arithmetic (skips weekends)")
	friday := time.Date(2024, 1, 12, 10, 0, 0, 0, utc) // Friday
	z = zeit.New(friday, utc)
	fmt.Printf("   Start:     %s (%s)\n", z.Format("2006-01-02"), z.Time().Weekday())

	for i := 1; i <= 5; i++ {
		nextBusDay := z.AddBusinessDays(i)
		fmt.Printf("   +%d bus day: %s (%s)\n", i, nextBusDay.Format("2006-01-02"), nextBusDay.Time().Weekday())
	}

	// Example 6: Time comparisons
	fmt.Println()
	fmt.Println("6. Time Comparisons")
	time1 := zeit.New(time.Date(2024, 1, 1, 12, 0, 0, 0, utc), utc)
	time2 := zeit.New(time.Date(2024, 1, 2, 12, 0, 0, 0, utc), utc)

	fmt.Printf("   time1 (%s) before time2 (%s): %v\n",
		time1.Format("2006-01-02"), time2.Format("2006-01-02"), time1.Before(time2))
	fmt.Printf("   time2 after time1: %v\n", time2.After(time1))

	// Example 7: Billing cycles
	fmt.Println()
	fmt.Println("7. Billing Cycles")
	startDate := zeit.New(time.Date(2024, 1, 15, 0, 0, 0, 0, utc), utc)

	fmt.Println("   Monthly cycles (next 3 months):")
	cycles := startDate.Cycles(3, zeit.Monthly)
	for i, period := range cycles {
		fmt.Printf("     Cycle %d: %s to %s\n",
			i+1,
			period.StartsAt.Format("2006-01-02"),
			period.EndsAt.Format("2006-01-02"),
		)
	}

	fmt.Println("   Weekly cycles (next 4 weeks):")
	cycles = startDate.Cycles(4, zeit.Weekly)
	for i, period := range cycles {
		fmt.Printf("     Cycle %d: %s to %s\n",
			i+1,
			period.StartsAt.Format("2006-01-02"),
			period.EndsAt.Format("2006-01-02"),
		)
	}

	// Example 8: Timezone-aware operations
	fmt.Println()
	fmt.Println("8. Timezone Preservation")
	londonLoc, _ := time.LoadLocation("Europe/London")
	z = zeit.New(time.Date(2024, 6, 15, 14, 30, 0, 0, londonLoc), londonLoc)
	fmt.Printf("   Created in London: %s\n", z.ToUser())
	fmt.Printf("   Location: %s\n", z.Location())

	// Add days preserves timezone
	tomorrow = z.AddDays(1)
	fmt.Printf("   Tomorrow in London: %s\n", tomorrow.ToUser())
	fmt.Printf("   Location preserved: %v\n", tomorrow.Location() == londonLoc)

	// Example 9: Parsing from user input
	fmt.Println()
	fmt.Println("9. Parsing ISO Date Strings")
	isoString := "2024-06-15T14:30:00Z"
	parsed, err := zeit.FromUser(isoString, utc)
	if err != nil {
		log.Fatalf("Failed to parse: %v", err)
	}
	fmt.Printf("   Parsed: %s\n", parsed.ToUser())
	fmt.Printf("   Timestamp: %d\n", parsed.Unix())

	// Example 10: Durations
	fmt.Println()
	fmt.Println("10. Duration Calculations")
	start := zeit.New(time.Date(2024, 1, 1, 0, 0, 0, 0, utc), utc)
	end := zeit.New(time.Date(2024, 1, 10, 12, 30, 0, 0, utc), utc)

	duration := zeit.NewDuration(start, end)
	fmt.Printf("   From %s to %s:\n",
		start.Format("2006-01-02"),
		end.Format("2006-01-02"),
	)
	fmt.Printf("     Days: %d\n", duration.Days())
	fmt.Printf("     Business Days: %d (Mon-Fri only)\n", duration.BusinessDays())
	fmt.Printf("     Hours: %d\n", duration.Hours())

	fmt.Println()
	fmt.Println("✅ Zeit examples complete!")
}
