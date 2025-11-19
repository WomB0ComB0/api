-- Enable extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pg_trgm";

-- Create schemas
CREATE SCHEMA IF NOT EXISTS core;
CREATE SCHEMA IF NOT EXISTS media;
CREATE SCHEMA IF NOT EXISTS links;

-- Core tables
CREATE TABLE IF NOT EXISTS core.users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email VARCHAR(255) UNIQUE NOT NULL,
    name VARCHAR(255),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_users_email ON core.users(email);

-- Media tables
CREATE TABLE IF NOT EXISTS media.assets (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES core.users(id) ON DELETE CASCADE,
    filename VARCHAR(512) NOT NULL,
    mime_type VARCHAR(100) NOT NULL,
    size_bytes BIGINT NOT NULL,
    r2_key VARCHAR(512) NOT NULL,
    public_url TEXT NOT NULL,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_assets_user_id ON media.assets(user_id);
CREATE INDEX idx_assets_r2_key ON media.assets(r2_key);
CREATE INDEX idx_assets_metadata ON media.assets USING gin(metadata);

-- Links tables
CREATE TABLE IF NOT EXISTS links.links (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES core.users(id) ON DELETE CASCADE,
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

CREATE INDEX idx_links_user_id ON links.links(user_id);
CREATE INDEX idx_links_slug ON links.links(slug);
CREATE INDEX idx_links_is_active ON links.links(is_active);

CREATE TABLE IF NOT EXISTS links.clicks (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    link_id UUID REFERENCES links.links(id) ON DELETE CASCADE,
    ip_address INET,
    user_agent TEXT,
    referer TEXT,
    country VARCHAR(2),
    clicked_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_clicks_link_id ON links.clicks(link_id);
CREATE INDEX idx_clicks_clicked_at ON links.clicks(clicked_at);

-- Function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Triggers for updated_at
CREATE TRIGGER update_users_updated_at BEFORE UPDATE ON core.users
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_assets_updated_at BEFORE UPDATE ON media.assets
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_links_updated_at BEFORE UPDATE ON links.links
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Increment click count trigger
CREATE OR REPLACE FUNCTION increment_link_clicks()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE links.links SET clicks = clicks + 1 WHERE id = NEW.link_id;
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER increment_clicks AFTER INSERT ON links.clicks
    FOR EACH ROW EXECUTE FUNCTION increment_link_clicks();
