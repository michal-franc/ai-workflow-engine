package health

import (
	"context"
	"errors"
	"time"
)

// BackfillDay is one past day Backfill looked at. Skipped says why nothing
// was written: a snapshot from that day exists, or the branch had no commit
// yet.
type BackfillDay struct {
	Day     time.Time
	Path    string
	Skipped string
	Err     error
}

// backfillHour and backfillMinute place a backfilled snapshot at the end of
// its day, where the daily one would have seen the whole day.
const backfillHour, backfillMinute = 23, 59

// Backfill takes one snapshot for each of the days before env.Now (oldest
// first), as the families would have measured it at the end of that day:
// Now is 23:59 local, Commit where env.Branch stood then. Days that already
// have a snapshot are skipped. each is called after every day; Backfill
// stops early when ctx ends.
func Backfill(ctx context.Context, env Env, days int, each func(BackfillDay)) error {
	return backfill(ctx, env, days, families(), each)
}

func backfill(ctx context.Context, env Env, days int, fams []registered, each func(BackfillDay)) error {
	if env.Root == "" {
		return errors.New("health: no project root")
	}
	now := env.Now
	if now.IsZero() {
		now = time.Now()
	}
	today := now.Local()
	for d := days; d >= 1; d-- {
		if err := ctx.Err(); err != nil {
			return err
		}
		y, m, dd := today.AddDate(0, 0, -d).Date()
		at := time.Date(y, m, dd, backfillHour, backfillMinute, 0, 0, time.Local)
		day := BackfillDay{Day: at}
		if HasDay(env.Root, at) {
			day.Skipped = "has a snapshot"
			each(day)
			continue
		}
		commit, _, err := commitBefore(ctx, env.Root, env.Branch, env.Commit, at)
		if err != nil {
			day.Err = err
			each(day)
			continue
		}
		if commit == "" {
			day.Skipped = "no commit on " + env.Branch + " yet"
			each(day)
			continue
		}
		e := env
		e.Now, e.Commit, e.Backfill = at, commit, true
		_, day.Path, day.Err = take(ctx, e, TriggerBackfill, fams)
		each(day)
	}
	return nil
}
