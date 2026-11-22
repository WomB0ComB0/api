import type { Database } from './db.js';

const BOOTSTRAP_SQL = `
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pg_trgm";

CREATE SCHEMA IF NOT EXISTS core;
CREATE SCHEMA IF NOT EXISTS media;
CREATE SCHEMA IF NOT EXISTS links;

CREATE TABLE IF NOT EXISTS core.users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email VARCHAR(255) UNIQUE NOT NULL,
    name VARCHAR(255) NOT NULL,
    password_hash TEXT NOT NULL,
    role VARCHAR(32) NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'admin')),
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_email ON core.users(email);
CREATE INDEX IF NOT EXISTS idx_users_role ON core.users(role);
CREATE INDEX IF NOT EXISTS idx_users_created_at ON core.users(created_at DESC);

CREATE TABLE IF NOT EXISTS core.refresh_tokens (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES core.users(id) ON DELETE CASCADE,
    token_hash VARCHAR(64) UNIQUE NOT NULL,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    revoked_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON core.refresh_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_expires_at ON core.refresh_tokens(expires_at);

CREATE TABLE IF NOT EXISTS core.api_keys (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES core.users(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    key_prefix VARCHAR(16) NOT NULL,
    key_hash VARCHAR(64) UNIQUE NOT NULL,
    scopes TEXT[] NOT NULL DEFAULT ARRAY['read'],
    last_used_at TIMESTAMP WITH TIME ZONE,
    expires_at TIMESTAMP WITH TIME ZONE,
    revoked_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON core.api_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_user_active ON core.api_keys(user_id, created_at DESC) WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_api_keys_key_hash ON core.api_keys(key_hash);

CREATE TABLE IF NOT EXISTS media.assets (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES core.users(id) ON DELETE CASCADE,
    filename VARCHAR(512) NOT NULL,
    mime_type VARCHAR(100) NOT NULL,
    size_bytes BIGINT NOT NULL CHECK (size_bytes >= 0),
    r2_key VARCHAR(512) UNIQUE NOT NULL,
    public_url TEXT NOT NULL,
    visibility VARCHAR(16) NOT NULL DEFAULT 'public' CHECK (visibility IN ('public', 'private')),
    etag VARCHAR(128),
    sha256 VARCHAR(64),
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_assets_user_id ON media.assets(user_id);
CREATE INDEX IF NOT EXISTS idx_assets_user_created ON media.assets(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_assets_r2_key ON media.assets(r2_key);
CREATE INDEX IF NOT EXISTS idx_assets_visibility ON media.assets(visibility);
CREATE INDEX IF NOT EXISTS idx_assets_metadata ON media.assets USING gin(metadata);

CREATE TABLE IF NOT EXISTS links.links (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES core.users(id) ON DELETE CASCADE,
    slug VARCHAR(100) UNIQUE NOT NULL,
    target_url TEXT NOT NULL,
    title VARCHAR(255),
    description TEXT,
    clicks INTEGER DEFAULT 0,
    is_active BOOLEAN DEFAULT true,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_links_user_id ON links.links(user_id);
CREATE INDEX IF NOT EXISTS idx_links_user_created ON links.links(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_links_slug ON links.links(slug);
CREATE INDEX IF NOT EXISTS idx_links_is_active ON links.links(is_active);

CREATE TABLE IF NOT EXISTS links.clicks (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    link_id UUID NOT NULL REFERENCES links.links(id) ON DELETE CASCADE,
    ip_address INET,
    user_agent TEXT,
    referer TEXT,
    country VARCHAR(2),
    clicked_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_clicks_link_id ON links.clicks(link_id);
CREATE INDEX IF NOT EXISTS idx_clicks_link_clicked ON links.clicks(link_id, clicked_at DESC);
CREATE INDEX IF NOT EXISTS idx_clicks_clicked_at ON links.clicks(clicked_at);

CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS update_users_updated_at ON core.users;
CREATE TRIGGER update_users_updated_at BEFORE UPDATE ON core.users
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_assets_updated_at ON media.assets;
CREATE TRIGGER update_assets_updated_at BEFORE UPDATE ON media.assets
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_links_updated_at ON links.links;
CREATE TRIGGER update_links_updated_at BEFORE UPDATE ON links.links
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE OR REPLACE FUNCTION increment_link_clicks()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE links.links SET clicks = clicks + 1 WHERE id = NEW.link_id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS increment_clicks ON links.clicks;
CREATE TRIGGER increment_clicks AFTER INSERT ON links.clicks
    FOR EACH ROW EXECUTE FUNCTION increment_link_clicks();
`;

export async function ensureSchema(db: Database): Promise<void> {
  await db.query(BOOTSTRAP_SQL);
}
