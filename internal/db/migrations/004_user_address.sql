-- Home address, shown on the profile. Optional, so existing users get ''.
ALTER TABLE users ADD COLUMN address TEXT NOT NULL DEFAULT '';
