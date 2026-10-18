-- A memory entry a session review suggested waits for the owner's approval;
-- runs see only approved entries.
ALTER TABLE memories ADD COLUMN pending INTEGER NOT NULL DEFAULT 0;
