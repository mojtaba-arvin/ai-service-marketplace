-- Bind scheduled orders to the tenant that owns the customer workflow.
-- Legacy orders without an unambiguous customer membership remain NULL and are
-- intentionally excluded from tenant-scoped operator queues until explicitly
-- reconciled; guessing their tenant would create a cross-tenant data leak.
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS tenant_id text;

UPDATE orders AS o
SET tenant_id = membership.tenant_id
FROM (
    SELECT identity_id, MIN(tenant_id) AS tenant_id
    FROM identity_memberships
    WHERE role = 'customer'
    GROUP BY identity_id
    HAVING COUNT(*) = 1
) AS membership
WHERE o.customer_id = membership.identity_id
  AND o.tenant_id IS NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'orders_tenant_id_nonempty'
          AND conrelid = 'orders'::regclass
    ) THEN
        ALTER TABLE orders
            ADD CONSTRAINT orders_tenant_id_nonempty
            CHECK (tenant_id IS NULL OR length(btrim(tenant_id)) BETWEEN 1 AND 120);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS orders_tenant_market_created_idx
    ON orders(tenant_id, country_code, created_at DESC, id DESC)
    WHERE tenant_id IS NOT NULL;
