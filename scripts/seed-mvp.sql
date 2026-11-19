-- Example single-market seed for the MVP.
-- Run only after migrations have completed. Replace the example market, tenant,
-- phones, price, and service area with the actual launch configuration.
BEGIN;

INSERT INTO markets(code,default_locale,currency,timezone,policy_version,enabled)
VALUES (:'market_code',:'locale',:'currency',:'timezone',:'policy_version',true)
ON CONFLICT(code) DO UPDATE SET
    default_locale=EXCLUDED.default_locale,
    currency=EXCLUDED.currency,
    timezone=EXCLUDED.timezone,
    policy_version=EXCLUDED.policy_version,
    enabled=true,
    version=markets.version+1,
    updated_at=now();

INSERT INTO market_locales(market_code,locale,is_default)
VALUES (:'market_code',:'locale',true)
ON CONFLICT(market_code,locale) DO UPDATE SET is_default=true;

INSERT INTO service_categories(id,parent_id,slug,display_name,enabled)
VALUES (:'category_id',NULL,:'category_slug',:'category_name',true)
ON CONFLICT(id) DO UPDATE SET display_name=EXCLUDED.display_name,enabled=true,updated_at=now();

INSERT INTO market_category_visibility(market_code,category_id,visible)
VALUES (:'market_code',:'category_id',true)
ON CONFLICT(market_code,category_id) DO UPDATE SET visible=true,updated_at=now();

INSERT INTO category_translations(market_code,category_id,locale,display_name,description)
VALUES (:'market_code',:'category_id',:'locale',:'category_name',:'category_description')
ON CONFLICT(market_code,category_id,locale) DO UPDATE
SET display_name=EXCLUDED.display_name,description=EXCLUDED.description,updated_at=now();

INSERT INTO service_price_menus(id,category_id,service_code)
VALUES (:'menu_id',:'category_id',:'service_code')
ON CONFLICT(category_id,service_code) DO NOTHING;

INSERT INTO service_price_versions(id,menu_id,version_no,amount_minor,currency)
VALUES (:'price_version_id',:'menu_id',1,:'price_minor',:'currency')
ON CONFLICT(menu_id,version_no) DO NOTHING;

UPDATE service_price_menus
SET active_version_id=:'price_version_id',updated_at=now()
WHERE id=:'menu_id';

INSERT INTO phone_identities(id,phone_e164)
VALUES ('mvp-operator-identity',:'operator_phone')
ON CONFLICT(phone_e164) DO NOTHING;

INSERT INTO identity_memberships(id,identity_id,tenant_id,role)
SELECT 'mvp-operator-membership',id,:'tenant_id','operator'
FROM phone_identities WHERE phone_e164=:'operator_phone'
ON CONFLICT(identity_id,tenant_id,role) DO NOTHING;

INSERT INTO cancellation_policies(
    id,market_code,policy_version,actor_role,from_status,cutoff_minutes,
    fee_basis_points,effective_from,created_by_identity_id
)
SELECT 'mvp-cancel-requested',:'market_code',:'policy_version','customer','requested',
       0,0,now()-interval '1 day',id
FROM phone_identities WHERE phone_e164=:'operator_phone'
ON CONFLICT(market_code,policy_version,actor_role,from_status) DO NOTHING;

INSERT INTO cancellation_policies(
    id,market_code,policy_version,actor_role,from_status,cutoff_minutes,
    fee_basis_points,effective_from,created_by_identity_id
)
SELECT 'mvp-cancel-accepted',:'market_code',:'policy_version','customer','accepted',
       0,0,now()-interval '1 day',id
FROM phone_identities WHERE phone_e164=:'operator_phone'
ON CONFLICT(market_code,policy_version,actor_role,from_status) DO NOTHING;

INSERT INTO phone_identities(id,phone_e164)
VALUES ('mvp-professional-identity',:'professional_phone')
ON CONFLICT(phone_e164) DO NOTHING;

INSERT INTO identity_memberships(id,identity_id,tenant_id,role)
SELECT 'mvp-professional-membership',id,:'tenant_id','professional'
FROM phone_identities WHERE phone_e164=:'professional_phone'
ON CONFLICT(identity_id,tenant_id,role) DO NOTHING;

INSERT INTO provider_profiles(id,identity_id,tenant_id,display_name,active)
SELECT 'mvp-professional-profile',id,:'tenant_id',:'professional_name',false
FROM phone_identities WHERE phone_e164=:'professional_phone'
ON CONFLICT(identity_id) DO UPDATE
SET tenant_id=EXCLUDED.tenant_id,display_name=EXCLUDED.display_name,updated_at=now();

INSERT INTO provider_skills(id,provider_id,category_id)
SELECT 'mvp-professional-skill',p.id,:'category_id'
FROM provider_profiles p JOIN phone_identities i ON i.id=p.identity_id
WHERE i.phone_e164=:'professional_phone'
ON CONFLICT(provider_id,category_id) DO NOTHING;

INSERT INTO provider_service_areas(id,provider_id,country_code,region_code,city_code,area_code,active)
SELECT 'mvp-professional-area',p.id,:'market_code',:'region_code',:'city_code',:'area_code',true
FROM provider_profiles p JOIN phone_identities i ON i.id=p.identity_id
WHERE i.phone_e164=:'professional_phone'
ON CONFLICT(provider_id,country_code,region_code,city_code,area_code) DO UPDATE SET active=true;

INSERT INTO provider_availability_windows(id,provider_id,window_type,start_at,end_at,timezone,created_by_identity_id)
SELECT 'mvp-professional-availability',p.id,'available',
       now()-interval '1 minute',now()+interval '30 days',:'timezone',i.id
FROM provider_profiles p JOIN phone_identities i ON i.id=p.identity_id
WHERE i.phone_e164=:'professional_phone'
ON CONFLICT(id) DO NOTHING;

COMMIT;
