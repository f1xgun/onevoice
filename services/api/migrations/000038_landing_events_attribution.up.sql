BEGIN;

CREATE TABLE landing_events (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    cta TEXT NOT NULL CHECK (cta IN ('hero-waitlist', 'hero-register', 'nav-register', 'nav-login', 'pricing-free-register', 'pricing-pro-waitlist', 'waitlist-success-register')),
    path TEXT NOT NULL CHECK (octet_length(path) BETWEEN 1 AND 1024 AND left(path, 1) = '/' AND left(path, 2) <> '//' AND path !~ '[?#[:cntrl:]]' AND position(chr(92) in path) = 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE waitlist_signups
    ADD COLUMN source TEXT CHECK (source IN ('landing', 'billing', 'business-limit')),
    ADD COLUMN plan TEXT CHECK (plan IN ('pro'));

COMMIT;
