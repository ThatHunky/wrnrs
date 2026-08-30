CREATE TABLE IF NOT EXISTS users (
	telegram_id INTEGER PRIMARY KEY,
	username_normalized TEXT,
	display_name TEXT NOT NULL DEFAULT '',
	gender TEXT NOT NULL DEFAULT '',
	language TEXT NOT NULL DEFAULT 'uk',
	phone_e164_encrypted BLOB,
	phone_lookup_hash TEXT,
	is_18_plus BOOLEAN NOT NULL DEFAULT 0,
	adult_confirmed_at TEXT,
	mature_opt_in BOOLEAN NOT NULL DEFAULT 0,
	mature_opted_in_at TEXT,
	theme_base_color TEXT,
	selected_style_id TEXT NOT NULL DEFAULT 'default_warm',
	selected_background_asset_id TEXT,
	selected_font_id TEXT NOT NULL DEFAULT 'nunito_regular',
	custom_border_radius INTEGER,
	custom_glass_opacity REAL,
	onboarding_status TEXT NOT NULL DEFAULT 'new',
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS users_username_normalized_idx
	ON users(username_normalized)
	WHERE username_normalized IS NOT NULL AND username_normalized != '';

CREATE INDEX IF NOT EXISTS users_phone_lookup_hash_idx
	ON users(phone_lookup_hash)
	WHERE phone_lookup_hash IS NOT NULL;

CREATE TABLE IF NOT EXISTS pairs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_a_id INTEGER NOT NULL REFERENCES users(telegram_id) ON DELETE CASCADE,
	user_b_id INTEGER NOT NULL REFERENCES users(telegram_id) ON DELETE CASCADE,
	status TEXT NOT NULL DEFAULT 'active',
	active_level INTEGER NOT NULL DEFAULT 1,
	highest_unlocked_level INTEGER NOT NULL DEFAULT 1,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	ended_at TEXT,
	CHECK(user_a_id < user_b_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS pairs_unique_users_idx
	ON pairs(user_a_id, user_b_id);

CREATE UNIQUE INDEX IF NOT EXISTS pairs_one_active_user_a_idx
	ON pairs(user_a_id)
	WHERE status = 'active';

CREATE UNIQUE INDEX IF NOT EXISTS pairs_one_active_user_b_idx
	ON pairs(user_b_id)
	WHERE status = 'active';

CREATE TABLE IF NOT EXISTS pair_requests (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	requester_id INTEGER NOT NULL REFERENCES users(telegram_id) ON DELETE CASCADE,
	target_telegram_id INTEGER,
	target_username_normalized TEXT,
	target_phone_hash TEXT,
	invite_token TEXT NOT NULL UNIQUE,
	status TEXT NOT NULL DEFAULT 'pending',
	expires_at TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS game_sessions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	pair_id INTEGER NOT NULL REFERENCES pairs(id) ON DELETE CASCADE,
	level INTEGER NOT NULL,
	question_id TEXT NOT NULL,
	question_source TEXT NOT NULL DEFAULT 'stock',
	question_text_uk TEXT NOT NULL DEFAULT '',
	question_text_en TEXT NOT NULL DEFAULT '',
	requires_mature_opt_in BOOLEAN NOT NULL DEFAULT 0,
	status TEXT NOT NULL DEFAULT 'active',
	deck_cycle INTEGER NOT NULL DEFAULT 0,
	invited_by_user_id INTEGER,
	accepted_by_user_id INTEGER,
	invite_expires_at TEXT,
	started_at TEXT,
	revealed_at TEXT,
	completed_at TEXT,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS game_sessions_one_current_pair_idx
	ON game_sessions(pair_id)
	WHERE status IN ('pending_acceptance', 'active', 'revealed');

CREATE TABLE IF NOT EXISTS game_answers (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id INTEGER NOT NULL REFERENCES game_sessions(id) ON DELETE CASCADE,
	user_id INTEGER NOT NULL REFERENCES users(telegram_id) ON DELETE CASCADE,
	completion_type TEXT NOT NULL,
	answer_text_encrypted BLOB,
	completed_at TEXT NOT NULL,
	revealed_at TEXT,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(session_id, user_id)
);

CREATE TABLE IF NOT EXISTS pair_card_history (
	pair_id INTEGER NOT NULL REFERENCES pairs(id) ON DELETE CASCADE,
	question_id TEXT NOT NULL,
	level INTEGER NOT NULL,
	deck_cycle INTEGER NOT NULL DEFAULT 0,
	completed_at TEXT NOT NULL,
	PRIMARY KEY(pair_id, question_id, deck_cycle)
);

CREATE TABLE IF NOT EXISTS pair_support_prompt_state (
	pair_id INTEGER PRIMARY KEY,
	last_prompted_at TEXT,
	last_prompt_message_id INTEGER,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS theme_assets (
	id TEXT PRIMARY KEY,
	owner_user_id INTEGER REFERENCES users(telegram_id) ON DELETE CASCADE,
	kind TEXT NOT NULL,
	minio_object_key TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'active',
	width INTEGER NOT NULL DEFAULT 0,
	height INTEGER NOT NULL DEFAULT 0,
	size_bytes INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS pair_theme_shares (
	pair_id INTEGER NOT NULL REFERENCES pairs(id) ON DELETE CASCADE,
	asset_id TEXT NOT NULL REFERENCES theme_assets(id) ON DELETE CASCADE,
	shared_by_user_id INTEGER NOT NULL REFERENCES users(telegram_id) ON DELETE CASCADE,
	status TEXT NOT NULL DEFAULT 'active',
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY(pair_id, asset_id)
);

CREATE TABLE IF NOT EXISTS purchase_receipts (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(telegram_id) ON DELETE CASCADE,
	sku TEXT NOT NULL,
	currency TEXT NOT NULL DEFAULT 'XTR',
	stars_amount INTEGER NOT NULL,
	telegram_payment_charge_id TEXT NOT NULL UNIQUE,
	provider_payment_charge_id TEXT,
	status TEXT NOT NULL DEFAULT 'successful',
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS entitlements (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL REFERENCES users(telegram_id) ON DELETE CASCADE,
	unlock_type TEXT NOT NULL,
	unlock_id TEXT NOT NULL,
	source TEXT NOT NULL,
	source_receipt_id INTEGER REFERENCES purchase_receipts(id) ON DELETE SET NULL,
	expires_at TEXT,
	active BOOLEAN NOT NULL DEFAULT 1,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	UNIQUE(user_id, unlock_type, unlock_id)
);

CREATE TABLE IF NOT EXISTS admin_audit_log (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	admin_user_id INTEGER NOT NULL,
	target_user_id INTEGER NOT NULL,
	action TEXT NOT NULL,
	unlock_type TEXT NOT NULL,
	unlock_id TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS custom_questions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	creator_id INTEGER NOT NULL REFERENCES users(telegram_id) ON DELETE CASCADE,
	question_text TEXT NOT NULL,
	created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
	deleted_at TEXT
);

CREATE INDEX IF NOT EXISTS custom_questions_creator_id_idx ON custom_questions(creator_id);

CREATE TABLE IF NOT EXISTS pair_position_marks (
	pair_id      INTEGER NOT NULL REFERENCES pairs(id) ON DELETE CASCADE,
	position_id  TEXT    NOT NULL,
	tried_at     TIMESTAMP,
	favorited_at TIMESTAMP,
	hidden_at    TIMESTAMP,
	marked_by    INTEGER REFERENCES users(telegram_id) ON DELETE SET NULL,
	updated_at   TIMESTAMP NOT NULL,
	PRIMARY KEY (pair_id, position_id)
);
