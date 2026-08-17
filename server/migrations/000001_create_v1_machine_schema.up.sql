CREATE TABLE machines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 255),
    hostname TEXT NOT NULL CHECK (length(trim(hostname)) BETWEEN 1 AND 255),
    operating_system TEXT NOT NULL CHECK (length(trim(operating_system)) > 0),
    os_version TEXT NOT NULL CHECK (length(trim(os_version)) > 0),
    architecture TEXT NOT NULL CHECK (length(trim(architecture)) > 0),
    agent_version TEXT NOT NULL CHECK (length(trim(agent_version)) > 0),
    status TEXT NOT NULL DEFAULT 'unknown'
        CHECK (status IN ('online', 'offline', 'degraded', 'unknown')),
    registered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ,
    last_connected_at TIMESTAMPTZ,
    last_disconnected_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX machines_status_last_seen_idx
    ON machines (status, last_seen_at DESC);

CREATE TABLE agent_identities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    machine_id UUID NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL UNIQUE,
    credential_hash BYTEA NOT NULL,
    capabilities JSONB NOT NULL DEFAULT '[]'::jsonb
        CHECK (jsonb_typeof(capabilities) = 'array'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_connected_at TIMESTAMPTZ,
    last_rotated_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX agent_identities_one_active_machine_idx
    ON agent_identities (machine_id)
    WHERE revoked_at IS NULL;

CREATE TABLE machine_presence_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    machine_id UUID NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (status IN ('online', 'offline')),
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX machine_presence_events_machine_occurred_idx
    ON machine_presence_events (machine_id, occurred_at DESC);

CREATE TABLE telemetry_samples (
    machine_id UUID NOT NULL REFERENCES machines(id) ON DELETE CASCADE,
    collected_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    cpu_usage_percent NUMERIC(5, 2) NOT NULL
        CHECK (cpu_usage_percent >= 0 AND cpu_usage_percent <= 100),
    memory_total_bytes BIGINT NOT NULL CHECK (memory_total_bytes >= 0),
    memory_used_bytes BIGINT NOT NULL
        CHECK (memory_used_bytes BETWEEN 0 AND memory_total_bytes),
    disk_total_bytes BIGINT NOT NULL CHECK (disk_total_bytes >= 0),
    disk_used_bytes BIGINT NOT NULL
        CHECK (disk_used_bytes BETWEEN 0 AND disk_total_bytes),
    uptime_seconds BIGINT NOT NULL CHECK (uptime_seconds >= 0),
    PRIMARY KEY (machine_id, collected_at)
);

CREATE INDEX telemetry_samples_collected_at_idx
    ON telemetry_samples (collected_at);
