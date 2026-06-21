package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/store"
)

// memoryStats is a snapshot of the memory tables for observability.
type memoryStats struct {
	// Procedural (L3)
	ProcTotal    int
	ProcEmbedded int // has a non-empty embedding → eligible for embedding recall
	ProcArchived int
	ProcActive   int // usage_count > 0 (has ever been recalled)
	ProcDormant  int // usage_count == 0
	EffLow       int // effectiveness < 0.3
	EffMid       int // 0.3 <= effectiveness < 0.7
	EffHigh      int // effectiveness >= 0.7
	AvgEff       float64
	TopUsage     int // highest usage_count
	// Episodic (L1)
	EpiTotal    int
	EpiArchived int
	// Semantic (L2)
	SemTotal    int
	SemArchived int
	// Recall activity
	UsageTotal          int // memory_usage rows = recall-and-use events
	UsageUnconsolidated int
}

// gatherMemoryStats reads a read-only snapshot of the memory tables.
func gatherMemoryStats(ctx context.Context, s *store.Store) (*memoryStats, error) {
	db := s.DB()
	st := &memoryStats{}
	q := func(sql string, dest ...any) error {
		return db.QueryRowContext(ctx, sql).Scan(dest...)
	}
	if err := q(`SELECT COUNT(*) FROM memory_procedural`, &st.ProcTotal); err != nil {
		return nil, err
	}
	_ = q(`SELECT COUNT(*) FROM memory_procedural WHERE embedding IS NOT NULL AND embedding NOT IN ('[]','null')`, &st.ProcEmbedded)
	_ = q(`SELECT COUNT(*) FROM memory_procedural WHERE COALESCE(archived,0)=1`, &st.ProcArchived)
	_ = q(`SELECT COUNT(*) FROM memory_procedural WHERE usage_count>0`, &st.ProcActive)
	st.ProcDormant = st.ProcTotal - st.ProcActive
	_ = q(`SELECT COUNT(*) FROM memory_procedural WHERE effectiveness<0.3`, &st.EffLow)
	_ = q(`SELECT COUNT(*) FROM memory_procedural WHERE effectiveness>=0.3 AND effectiveness<0.7`, &st.EffMid)
	_ = q(`SELECT COUNT(*) FROM memory_procedural WHERE effectiveness>=0.7`, &st.EffHigh)
	if st.ProcTotal > 0 {
		_ = q(`SELECT COALESCE(AVG(effectiveness),0) FROM memory_procedural`, &st.AvgEff)
	}
	_ = q(`SELECT COALESCE(MAX(usage_count),0) FROM memory_procedural`, &st.TopUsage)
	_ = q(`SELECT COUNT(*) FROM memory_episodic`, &st.EpiTotal)
	_ = q(`SELECT COUNT(*) FROM memory_episodic WHERE COALESCE(archived,0)=1`, &st.EpiArchived)
	_ = q(`SELECT COUNT(*) FROM memory_semantic`, &st.SemTotal)
	_ = q(`SELECT COUNT(*) FROM memory_semantic WHERE COALESCE(archived,0)=1`, &st.SemArchived)
	_ = q(`SELECT COUNT(*) FROM memory_usage`, &st.UsageTotal)
	_ = q(`SELECT COUNT(*) FROM memory_usage WHERE consolidated_at IS NULL`, &st.UsageUnconsolidated)
	return st, nil
}

func memoryStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show memory system statistics (recall utilization, effectiveness, archival)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			s, err := store.New(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer func() { _ = s.Close() }()
			ctx := context.Background()
			if err := store.RunMigrations(ctx, s.DB(), cfg.MigrationDir); err != nil {
				return fmt.Errorf("run migrations: %w", err)
			}
			st, err := gatherMemoryStats(ctx, s)
			if err != nil {
				return err
			}
			printMemoryStats(st)
			return nil
		},
	}
}

func printMemoryStats(st *memoryStats) {
	utilization := 0.0
	if st.ProcTotal > 0 {
		utilization = float64(st.ProcActive) / float64(st.ProcTotal) * 100
	}
	fmt.Println("Memory Statistics:")
	fmt.Println("==================")
	fmt.Println("Procedural (L3):")
	fmt.Printf("  total: %d (embedding-eligible: %d)\n", st.ProcTotal, st.ProcEmbedded)
	fmt.Printf("  active (usage>0): %d / dormant: %d\n", st.ProcActive, st.ProcDormant)
	fmt.Printf("  archived: %d\n", st.ProcArchived)
	fmt.Printf("  effectiveness: <0.3: %d | 0.3-0.7: %d | >=0.7: %d (avg %.2f)\n",
		st.EffLow, st.EffMid, st.EffHigh, st.AvgEff)
	fmt.Printf("  top recalled usage_count: %d\n", st.TopUsage)
	fmt.Printf("Episodic (L1): %d (archived: %d)\n", st.EpiTotal, st.EpiArchived)
	fmt.Printf("Semantic (L2): %d (archived: %d)\n", st.SemTotal, st.SemArchived)
	fmt.Printf("Recall activity: %d recall-use events (%d unconsolidated)\n",
		st.UsageTotal, st.UsageUnconsolidated)
	fmt.Printf("Recall utilization: %.0f%% of procedural memory ever recalled\n", utilization)
}
