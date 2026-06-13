-- Add embedding columns to memory_semantic for L2 semantic search.
ALTER TABLE memory_semantic ADD COLUMN embedding TEXT NOT NULL DEFAULT '[]';
ALTER TABLE memory_semantic ADD COLUMN embedding_model TEXT NOT NULL DEFAULT '';
