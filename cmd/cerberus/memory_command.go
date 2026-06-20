package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
)

func memoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Inspect and manage cerberus memory",
		Long:  "Inspect and manage cerberus memory (procedural, semantic, episodic)",
	}
	cmd.AddCommand(memoryListCmd(), memoryShowCmd(), memoryPruneCmd(), memoryReembedCmd())
	return cmd
}

func memoryListCmd() *cobra.Command {
	var all bool
	var memoryType string

	c := &cobra.Command{
		Use:   "list",
		Short: "List memories",
		Long:  "List procedural memories (default) or specify --type for semantic/episodic",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			logger, _ := zap.NewProduction()
			defer func() { _ = logger.Sync() }()

			s, err := store.New(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer func() { _ = s.Close() }()

			ctx := context.Background()
			if err := store.RunMigrations(ctx, s.DB(), cfg.MigrationDir); err != nil {
				return fmt.Errorf("run migrations: %w", err)
			}

			// Default to procedural if no type specified
			if memoryType == "" {
				memoryType = "procedural"
			}

			switch memoryType {
			case "procedural":
				return listProcedural(ctx, s, all)
			case "semantic":
				return listSemantic(ctx, s, all)
			default:
				return fmt.Errorf("unsupported memory type: %s (supported: procedural, semantic)", memoryType)
			}
		},
	}
	c.Flags().BoolVar(&all, "all", false, "include archived memories")
	c.Flags().StringVar(&memoryType, "type", "procedural", "memory type: procedural, semantic")
	return c
}

func listProcedural(ctx context.Context, s *store.Store, showAll bool) error {
	rows, err := s.DB().QueryContext(ctx,
		`SELECT id, name, effectiveness, usage_count, COALESCE(archived,0)
		 FROM memory_procedural
		 ORDER BY effectiveness DESC`)
	if err != nil {
		return fmt.Errorf("query procedural memories: %w", err)
	}
	defer func() { _ = rows.Close() }()

	fmt.Println("Procedural Memories:")
	fmt.Println("=====================================")
	count := 0
	for rows.Next() {
		var id int64
		var name string
		var eff float64
		var usage int
		var archived int
		if err := rows.Scan(&id, &name, &eff, &usage, &archived); err != nil {
			return err
		}
		if archived == 1 && !showAll {
			continue
		}
		archivedFlag := ""
		if archived == 1 {
			archivedFlag = " [archived]"
		}
		fmt.Printf("[%d] %s eff=%.2f usage=%d%s\n", id, name, eff, usage, archivedFlag)
		count++
	}
	if count == 0 {
		fmt.Println("No procedural memories found.")
	}
	return nil
}

func listSemantic(ctx context.Context, s *store.Store, showAll bool) error {
	rows, err := s.DB().QueryContext(ctx,
		`SELECT id, SUBSTR(content,1,50), COALESCE(archived,0)
		 FROM memory_semantic
		 ORDER BY id`)
	if err != nil {
		return fmt.Errorf("query semantic memories: %w", err)
	}
	defer func() { _ = rows.Close() }()

	fmt.Println("Semantic Memories:")
	fmt.Println("=====================================")
	count := 0
	for rows.Next() {
		var id int64
		var content string
		var archived int
		if err := rows.Scan(&id, &content, &archived); err != nil {
			return err
		}
		if archived == 1 && !showAll {
			continue
		}
		archivedFlag := ""
		if archived == 1 {
			archivedFlag = " [archived]"
		}
		fmt.Printf("[%d] %s...%s\n", id, content, archivedFlag)
		count++
	}
	if count == 0 {
		fmt.Println("No semantic memories found.")
	}
	return nil
}

func memoryShowCmd() *cobra.Command {
	var id int64

	c := &cobra.Command{
		Use:   "show",
		Short: "Show full details of a memory",
		Long:  "Show full details of a procedural or semantic memory by ID",
		RunE: func(cmd *cobra.Command, args []string) error {
			if id == 0 {
				return fmt.Errorf("--id is required")
			}

			cfg := config.Load()
			logger, _ := zap.NewProduction()
			defer func() { _ = logger.Sync() }()

			s, err := store.New(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer func() { _ = s.Close() }()

			ctx := context.Background()
			if err := store.RunMigrations(ctx, s.DB(), cfg.MigrationDir); err != nil {
				return fmt.Errorf("run migrations: %w", err)
			}

			// Try procedural first
			row := s.DB().QueryRowContext(ctx,
				`SELECT id, name, condition, action, effectiveness, usage_count,
				         COALESCE(project_name,''), category, type,
				         COALESCE(archived,0), created_at,
				         COALESCE(embedding_model,'')
				  FROM memory_procedural WHERE id = ?`, id)

			var m struct {
				ID            int64
				Name          string
				Condition     string
				Action        string
				Effectiveness float64
				UsageCount    int
				ProjectName   string
				Category      string
				Type          string
				Archived      int
				CreatedAt     string
				EmbeddingModel string
			}

			err = row.Scan(&m.ID, &m.Name, &m.Condition, &m.Action, &m.Effectiveness,
				&m.UsageCount, &m.ProjectName, &m.Category, &m.Type, &m.Archived,
				&m.CreatedAt, &m.EmbeddingModel)

			if err == nil {
				fmt.Println("Procedural Memory:")
				fmt.Println("=====================================")
				fmt.Printf("ID: %d\n", m.ID)
				fmt.Printf("Name: %s\n", m.Name)
				fmt.Printf("Condition: %s\n", m.Condition)
				fmt.Printf("Action: %s\n", m.Action)
				fmt.Printf("Effectiveness: %.2f\n", m.Effectiveness)
				fmt.Printf("Usage Count: %d\n", m.UsageCount)
				fmt.Printf("Project: %s\n", m.ProjectName)
				fmt.Printf("Category: %s\n", m.Category)
				fmt.Printf("Type: %s\n", m.Type)
				fmt.Printf("Archived: %d\n", m.Archived)
				fmt.Printf("Created At: %s\n", m.CreatedAt)
				fmt.Printf("Embedding Model: %s\n", m.EmbeddingModel)
				return nil
			}

			// Try semantic
			sem, err := s.GetSemanticByID(ctx, id)
			if err == nil {
				fmt.Println("Semantic Memory:")
				fmt.Println("=====================================")
				fmt.Printf("ID: %d\n", sem.ID)
				fmt.Printf("Content: %s\n", sem.Content)
				fmt.Printf("Source: %s\n", sem.Source)
				fmt.Printf("Tags: %v\n", sem.Tags)
				fmt.Printf("Confidence: %.2f\n", sem.Confidence)
				fmt.Printf("Project: %s\n", sem.ProjectName)
				fmt.Printf("Created At: %s\n", sem.CreatedAt)
				fmt.Printf("Embedding Model: %s\n", sem.EmbeddingModel)
				return nil
			}

			return fmt.Errorf("memory with ID %d not found (procedural or semantic)", id)
		},
	}
	c.Flags().Int64Var(&id, "id", 0, "memory ID")
	c.MarkFlagRequired("id")
	return c
}

func memoryPruneCmd() *cobra.Command {
	var hard bool

	c := &cobra.Command{
		Use:   "prune",
		Short: "Prune old/low-effectiveness memories",
		Long:  "Archive (default) or delete (--hard) stale memories by governance policy",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			logger, _ := zap.NewProduction()
			defer func() { _ = logger.Sync() }()

			s, err := store.New(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer func() { _ = s.Close() }()

			ctx := context.Background()
			if err := store.RunMigrations(ctx, s.DB(), cfg.MigrationDir); err != nil {
				return fmt.Errorf("run migrations: %w", err)
			}

			// Load project config to get project name
			projectName := "default"
			if cfg.Paths != nil && cfg.Paths.ConfigDir != "" {
				configPath := cfg.Paths.ConfigDir + "/project.yaml"
				projCfg, err := project.LoadFromFile(configPath)
				if err == nil && projCfg.Project.Name != "" {
					projectName = projCfg.Project.Name
				}
			}

			if hard {
				// Hard delete: physically remove archived rows
				res, err := s.DB().ExecContext(ctx, `DELETE FROM memory_procedural WHERE COALESCE(archived,0) = 1`)
				if err != nil {
					return fmt.Errorf("delete archived procedural: %w", err)
				}
				n1, _ := res.RowsAffected()

				res2, err := s.DB().ExecContext(ctx, `DELETE FROM memory_semantic WHERE COALESCE(archived,0) = 1`)
				if err != nil {
					return fmt.Errorf("delete archived semantic: %w", err)
				}
				n2, _ := res2.RowsAffected()

				fmt.Printf("Hard prune: deleted %d procedural + %d semantic archived memories\n", n1, n2)
				return nil
			}

			// Soft archive: mark stale memories
			n1, err := s.AutoArchiveLowEffectiveness(ctx, projectName)
			if err != nil {
				return fmt.Errorf("archive low effectiveness procedural: %w", err)
			}

			n2, err := s.ArchiveStaleEpisodic(ctx, 30)
			if err != nil {
				return fmt.Errorf("archive stale episodic: %w", err)
			}

			n3, err := s.ArchiveStaleSemantic(ctx, 90)
			if err != nil {
				return fmt.Errorf("archive stale semantic: %w", err)
			}

			fmt.Printf("Soft prune: archived %d procedural + %d episodic + %d semantic memories\n", n1, n2, n3)
			return nil
		},
	}
	c.Flags().BoolVar(&hard, "hard", false, "physically delete archived memories (default: soft archive)")
	return c
}

func memoryReembedCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "reembed",
		Short: "Re-embed all memories with current trigram model",
		Long:  "Re-embed ALL memory_procedural.condition and memory_semantic.content with the current trigram model, updating embedding + embedding_model. Use this to fix legacy empty-model rows.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.Load()
			logger, _ := zap.NewProduction()
			defer func() { _ = logger.Sync() }()

			s, err := store.New(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer func() { _ = s.Close() }()

			ctx := context.Background()
			if err := store.RunMigrations(ctx, s.DB(), cfg.MigrationDir); err != nil {
				return fmt.Errorf("run migrations: %w", err)
			}

			emb := embed.NewTrigramProvider(embed.DefaultDimension)
			modelName := emb.ModelName()

			// Re-embed procedural memories (condition field)
			procs, err := s.DB().QueryContext(ctx, `SELECT id, condition FROM memory_procedural`)
			if err != nil {
				return fmt.Errorf("query procedural memories: %w", err)
			}
			defer func() { _ = procs.Close() }()

			procCount := 0
			for procs.Next() {
				var id int64
				var cond string
				if err := procs.Scan(&id, &cond); err != nil {
					return err
				}
				vec, err := emb.Embed(ctx, cond)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to embed procedural %d: %v\n", id, err)
					continue
				}
				if err := s.UpdateProceduralEmbedding(ctx, id, vec, modelName); err != nil {
					return fmt.Errorf("update procedural embedding %d: %w", id, err)
				}
				procCount++
			}
			if err := procs.Err(); err != nil {
				return err
			}

			// Re-embed semantic memories (content field)
			sems, err := s.DB().QueryContext(ctx, `SELECT id, content FROM memory_semantic`)
			if err != nil {
				return fmt.Errorf("query semantic memories: %w", err)
			}
			defer func() { _ = sems.Close() }()

			semCount := 0
			for sems.Next() {
				var id int64
				var content string
				if err := sems.Scan(&id, &content); err != nil {
					return err
				}
				vec, err := emb.Embed(ctx, content)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to embed semantic %d: %v\n", id, err)
					continue
				}
				if err := s.UpdateSemanticEmbedding(ctx, id, vec, modelName); err != nil {
					return fmt.Errorf("update semantic embedding %d: %w", id, err)
				}
				semCount++
			}
			if err := sems.Err(); err != nil {
				return err
			}

			fmt.Printf("Re-embedded %d procedural + %d semantic memories with model %s\n", procCount, semCount, modelName)
			return nil
		},
	}
	return c
}
