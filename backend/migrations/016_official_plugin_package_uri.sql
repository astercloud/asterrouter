ALTER TABLE official_plugin_packages
  ADD COLUMN IF NOT EXISTS package_uri TEXT NOT NULL DEFAULT '';
