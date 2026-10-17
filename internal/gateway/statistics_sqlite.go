package gateway

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

var statisticsColumns = []string{
	"requests", "succeeded", "failed", "canceled", "input_total", "input_observations", "output_total", "output_observations",
	"cached_total", "cached_observations", "reasoning_total", "reasoning_observations", "total_ms", "total_ms_observations",
	"first_output_ms", "first_output_ms_observations", "cache_input", "cache_cached", "cache_observations",
}

func (s *Statistics) open(path string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("statistics database requires a clean absolute path")
	}
	if err := prepareStatisticsDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal", ".lock"} {
		if err := checkStatisticsFile(path + suffix); err != nil {
			return err
		}
	}
	// One gateway owns the store's health marker. This separate descriptor must
	// not open the database inode: closing it would release SQLite POSIX locks.
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	s.lock = lock
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("statistics database already in use: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err == nil {
		if err := file.Close(); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrExist) {
		return err
	}
	u := url.URL{Scheme: "file", Path: path}
	query := url.Values{"mode": {"rw"}, "_busy_timeout": {"1000"}, "_synchronous": {"FULL"}, "_defensive": {"1"}, "_pragma": {"trusted_schema=OFF"}}
	u.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return err
	}
	s.db = db
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version != 0 && version != 1 {
		return fmt.Errorf("unsupported statistics schema version %d", version)
	}
	if version == 0 {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master").Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return errors.New("unrecognized statistics database")
		}
	}
	var mode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
		return err
	}
	if mode != "wal" {
		return errors.New("statistics requires WAL journaling")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if version == 0 {
		columns := "day TEXT NOT NULL, model TEXT NOT NULL, application TEXT NOT NULL"
		for _, name := range statisticsColumns {
			columns += "," + name + " INTEGER NOT NULL CHECK(" + name + ">=0)"
		}
		if _, err := tx.ExecContext(ctx, "CREATE TABLE daily_usage ("+columns+", PRIMARY KEY(day,model,application)) WITHOUT ROWID; CREATE TABLE statistics_meta(id INTEGER PRIMARY KEY CHECK(id=1), recording_since TEXT NOT NULL, clean_shutdown INTEGER NOT NULL); CREATE TABLE statistics_health(day TEXT PRIMARY KEY, dropped INTEGER NOT NULL CHECK(dropped>=0), incomplete INTEGER NOT NULL) WITHOUT ROWID; PRAGMA user_version=1"); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO statistics_meta VALUES(1,?,1)", s.now().UTC().Format(time.DateOnly)); err != nil {
			return err
		}
	}
	var clean bool
	if err := tx.QueryRowContext(ctx, "SELECT clean_shutdown FROM statistics_meta WHERE id=1").Scan(&clean); err != nil {
		return err
	}
	if !clean {
		if _, err := tx.ExecContext(ctx, "INSERT INTO statistics_health(day,dropped,incomplete) VALUES(?,0,1) ON CONFLICT(day) DO UPDATE SET incomplete=1", s.now().UTC().Format(time.DateOnly)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE statistics_meta SET clean_shutdown=0 WHERE id=1"); err != nil {
		return err
	}
	cutoff := s.now().UTC().AddDate(0, 0, 1-s.retention).Format(time.DateOnly)
	if _, err := tx.ExecContext(ctx, "DELETE FROM daily_usage WHERE day < ?; DELETE FROM statistics_health WHERE day < ?", cutoff, cutoff); err != nil {
		return err
	}
	var dropped int64
	if err := tx.QueryRowContext(ctx, "SELECT coalesce(sum(dropped),0) FROM statistics_health").Scan(&dropped); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.dropped.Store(uint64(dropped))
	s.persistedDropped = uint64(dropped)
	s.maintenanceDay = s.now().UTC().Format(time.DateOnly)
	for _, suffix := range []string{"", "-wal", "-shm", ".lock"} {
		if err := checkStatisticsFile(path + suffix); err != nil {
			return err
		}
	}
	return nil
}

func prepareStatisticsDirectory(dir string) error {
	// Reject links throughout the path rather than resolving them into a
	// different storage scope. Existing broad parent directories are untouched.
	current := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(dir, current), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0700); err != nil {
				return err
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe statistics directory")
		}
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) || info.Mode().Perm() != 0700 {
		return errors.New("statistics directory must be owned by the current user with mode 0700")
	}
	var fs syscall.Statfs_t
	if err := syscall.Statfs(dir, &fs); err != nil {
		return err
	}
	// WAL shared memory is not safe on network filesystems. FUSE cannot prove
	// local locking semantics, so an explicit local path is required there too.
	switch uint64(fs.Type) {
	case 0x6969, 0xff534d42, 0xfe534d42, 0x5346414f, 0x9fa0, 0x65735546:
		return errors.New("statistics database requires a local filesystem")
	}
	return nil
}

func checkStatisticsFile(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || stat.Nlink != 1 || stat.Uid != uint32(os.Getuid()) || info.Mode().Perm() != 0600 {
		return fmt.Errorf("unsafe statistics file %s (requires owned singly linked regular file, mode 0600)", path)
	}
	return nil
}

func (s *Statistics) expire(ctx context.Context, tx *sql.Tx) error {
	cutoff := s.now().UTC().AddDate(0, 0, 1-s.retention).Format(time.DateOnly)
	_, err := tx.ExecContext(ctx, "DELETE FROM daily_usage WHERE day < ?; DELETE FROM statistics_health WHERE day < ?", cutoff, cutoff)
	return err
}

func (s *Statistics) writeBatch(ctx context.Context, events []statisticsEvent, closing bool) error {
	today := s.now().UTC().Format(time.DateOnly)
	if !closing && len(events) == 0 {
		if s.failed.Load() && s.now().Before(s.nextHealthAttempt) {
			return nil
		}
		if s.dropped.Load() == s.persistedDropped && s.maintenanceDay == today {
			return nil
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Merge within this bounded batch so a burst produces one row update per
	// day/model/application, never an archive of individual requests.
	merged := make(map[[3]string]statisticsCounters)
	for _, event := range events {
		key := [3]string{event.day, event.model, event.application}
		counters := merged[key]
		counters.add(event.counters)
		merged[key] = counters
	}
	updates := make([]string, 0, len(statisticsColumns))
	for _, name := range statisticsColumns {
		// Avoid SQLite promoting integer overflow to REAL and losing precision.
		updates = append(updates, name+"=CASE WHEN "+name+">9223372036854775807-excluded."+name+" THEN 9223372036854775807 ELSE "+name+"+excluded."+name+" END")
	}
	statement := "INSERT INTO daily_usage(day,model,application," + strings.Join(statisticsColumns, ",") + ") VALUES(" + strings.TrimSuffix(strings.Repeat("?,", 22), ",") + ") ON CONFLICT(day,model,application) DO UPDATE SET " + strings.Join(updates, ",")
	for key, counters := range merged {
		args := []any{key[0], key[1], key[2]}
		for _, value := range counters {
			args = append(args, statisticsInteger(value))
		}
		if _, err := tx.ExecContext(ctx, statement, args...); err != nil {
			return err
		}
	}
	dropped := s.dropped.Load()
	delta := dropped - s.persistedDropped
	if delta > 0 || s.failed.Load() {
		if _, err := tx.ExecContext(ctx, "INSERT INTO statistics_health(day,dropped,incomplete) VALUES(?,?,1) ON CONFLICT(day) DO UPDATE SET dropped=CASE WHEN dropped>9223372036854775807-excluded.dropped THEN 9223372036854775807 ELSE dropped+excluded.dropped END,incomplete=1", s.now().UTC().Format(time.DateOnly), statisticsInteger(delta)); err != nil {
			return err
		}
	}
	if err := s.expire(ctx, tx); err != nil {
		return err
	}
	var retained int64
	if err := tx.QueryRowContext(ctx, "SELECT coalesce(sum(dropped),0) FROM statistics_health").Scan(&retained); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE statistics_meta SET clean_shutdown=? WHERE id=1", closing && !s.failed.Load()); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// Concurrent queue overflows since the sampled value remain outstanding.
	s.dropped.Add(uint64(retained) - dropped)
	s.persistedDropped = uint64(retained)
	s.maintenanceDay = today
	return nil
}
