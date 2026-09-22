-- Lets a contact carry a label like "Mother" or "Best Friend".
-- DEFAULT '' so contacts created before this migration stay valid.
ALTER TABLE trusted_contacts ADD COLUMN relation TEXT NOT NULL DEFAULT '';
