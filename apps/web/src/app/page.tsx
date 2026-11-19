"use client";

import { useEffect, useMemo, useState, type FormEvent } from "react";

type Role = "customer" | "professional" | "operator";
type SessionTokens = { access_token: string; refresh_token: string };
type Me = { session_id: string; identity_id: string; phone: string; roles: Role[] };
type CatalogItem = {
  category_id: string;
  category_name: string;
  category_slug: string;
  menu_id: string;
  service_code: string;
  amount_minor: number;
  currency: string;
};
type Catalog = { market: string; currency: string; timezone: string; locale: string; items: CatalogItem[] };
type Order = {
  id: string;
  amount_minor: number;
  currency: string;
  status: string;
  scheduled_at?: string;
  country_code?: string;
  region_code?: string;
  city_code?: string;
  area_code?: string;
};
type OrderHistoryItem = { order: Order; created_at: string; updated_at: string };
type Offer = { id: string; order_id: string; start_at: string; end_at: string; expires_at: string };
type ProviderProfile = { id: string; display_name: string; active: boolean; verified_at?: string | null };
type Provider = {
  id: string;
  display_name: string;
  active: boolean;
  verified_at?: string | null;
  category_id?: string | null;
  skill_verified_at?: string | null;
};
type ProfessionalOrder = Order;
type ProfileResponse = { profile: ProviderProfile; qualification_complete: boolean };
type ApiError = { error?: string };

async function api<T>(path: string, token?: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (token) headers.set("Authorization", `Bearer ${token}`);
  if (init.body && !headers.has("Content-Type")) headers.set("Content-Type", "application/json");
  const response = await fetch(`/v1${path}`, { ...init, headers, cache: "no-store" });
  const payload = await response.json().catch(() => ({} as ApiError));
  if (!response.ok) {
    throw new Error((payload as ApiError).error || `Request failed (${response.status})`);
  }
  return payload as T;
}

function localDateTime(date: Date): string {
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function money(amountMinor: number, currency: string): string {
  return new Intl.NumberFormat(undefined, { style: "currency", currency }).format(amountMinor / 1000);
}

function friendlyDate(value?: string): string {
  if (!value) return "Not scheduled";
  return new Date(value).toLocaleString();
}

export default function HomePage() {
  const [phone, setPhone] = useState("");
  const [challengeId, setChallengeId] = useState("");
  const [otpCode, setOtpCode] = useState("");
  const [accessToken, setAccessToken] = useState("");
  const [refreshToken, setRefreshToken] = useState("");
  const [me, setMe] = useState<Me | null>(null);
  const [activeRole, setActiveRole] = useState<Role>("customer");
  const [catalog, setCatalog] = useState<Catalog | null>(null);
  const [orders, setOrders] = useState<OrderHistoryItem[]>([]);
  const [professionalOrders, setProfessionalOrders] = useState<ProfessionalOrder[]>([]);
  const [offers, setOffers] = useState<Offer[]>([]);
  const [providers, setProviders] = useState<Provider[]>([]);
  const [profile, setProfile] = useState<ProviderProfile | null>(null);
  const [qualificationComplete, setQualificationComplete] = useState(false);
  const [profileName, setProfileName] = useState("");
  const [selectedMenu, setSelectedMenu] = useState("");
  const [scheduledAt, setScheduledAt] = useState(() => localDateTime(new Date(Date.now() + 2 * 60 * 60 * 1000)));
  const [regionCode, setRegionCode] = useState("Muscat");
  const [cityCode, setCityCode] = useState("Muscat");
  const [areaCode, setAreaCode] = useState("Al-Khuwair");
  const [availabilityFrom, setAvailabilityFrom] = useState(() => localDateTime(new Date()));
  const [availabilityUntil, setAvailabilityUntil] = useState(() => localDateTime(new Date(Date.now() + 7 * 24 * 60 * 60 * 1000)));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const selectedItem = useMemo(
    () => catalog?.items.find((item) => item.menu_id === selectedMenu) ?? catalog?.items[0],
    [catalog, selectedMenu],
  );

  useEffect(() => {
    const savedAccess = window.sessionStorage.getItem("marketplace_access_token") || "";
    const savedRefresh = window.sessionStorage.getItem("marketplace_refresh_token") || "";
    if (!savedAccess) return;
    setAccessToken(savedAccess);
    setRefreshToken(savedRefresh);
    void api<Me>("/auth/me", savedAccess)
      .then((identity) => {
        setMe(identity);
        setActiveRole(identity.roles.includes("customer") ? "customer" : identity.roles[0] ?? "customer");
      })
      .catch(() => {
        window.sessionStorage.removeItem("marketplace_access_token");
        window.sessionStorage.removeItem("marketplace_refresh_token");
        setAccessToken("");
        setRefreshToken("");
      });
  }, []);

  useEffect(() => {
    if (!accessToken || !me) return;
    let active = true;
    async function load() {
      try {
        const data = await api<Catalog>("/catalog");
        if (!active) return;
        setCatalog(data);
        if (data.items.length > 0 && !selectedMenu) setSelectedMenu(data.items[0].menu_id);
        setCountryFromMarket(data.market);
        if (activeRole === "customer") {
          const history = await api<{ items: OrderHistoryItem[] }>("/orders?limit=20", accessToken);
          if (active) setOrders(history.items ?? []);
        } else if (activeRole === "professional") {
          const [offerData, orderData, profileData] = await Promise.all([
            api<{ offers: Offer[] }>("/professional/offers", accessToken),
            api<{ orders: ProfessionalOrder[] }>("/professional/orders", accessToken),
            api<ProfileResponse>("/professional/profile", accessToken).catch(() => null),
          ]);
          if (active) {
            setOffers(offerData.offers ?? []);
            setProfessionalOrders(orderData.orders ?? []);
            setProfile(profileData?.profile ?? null);
            setQualificationComplete(profileData?.qualification_complete ?? false);
          }
        } else if (activeRole === "operator") {
          const [orderData, providerData] = await Promise.all([
            api<{ orders: Order[] }>("/operator/orders", accessToken),
            api<{ providers: Provider[] }>("/operator/providers", accessToken),
          ]);
          if (active) {
            setOrders(orderData.orders.map((order) => ({ order, created_at: "", updated_at: "" }));
            setProviders(providerData.providers ?? []);
          }
        }
      } catch (reason) {
        if (active) setError(reason instanceof Error ? reason.message : "Could not load marketplace data.");
      }
    }
    void load();
    return () => { active = false; };
  }, [accessToken, me, activeRole, selectedMenu]);

  const [countryCode, setCountryCode] = useState("OM");

  function setCountryFromMarket(value: string) {
    setCountryCode(value);
  }

  async function requestCode(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true); setError(""); setNotice("");
    try {
      const result = await api<{ challenge_id: string }>("/auth/otp/request", undefined, {
        method: "POST", body: JSON.stringify({ phone }),
      });
      setChallengeId(result.challenge_id);
      setNotice("Verification code sent. Check your phone.");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Could not send verification code.");
    } finally { setBusy(false); }
  }

  async function verifyCode(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true); setError(""); setNotice("");
    try {
      const tokens = await api<SessionTokens>("/auth/otp/verify", undefined, {
        method: "POST", body: JSON.stringify({ challenge_id: challengeId, code: otpCode }),
      });
      window.sessionStorage.setItem("marketplace_access_token", tokens.access_token);
      window.sessionStorage.setItem("marketplace_refresh_token", tokens.refresh_token);
      setAccessToken(tokens.access_token); setRefreshToken(tokens.refresh_token);
      const identity = await api<Me>("/auth/me", tokens.access_token);
      setMe(identity);
      setActiveRole(identity.roles.includes("customer") ? "customer" : identity.roles[0] ?? "customer");
      setNotice("You are signed in.");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Verification failed.");
    } finally { setBusy(false); }
  }

  async function signOut() {
    setBusy(true);
    try {
      if (accessToken) await api("/auth/session/revoke", accessToken, { method: "POST" });
    } catch { /* local sign-out still clears the browser session */ }
    window.sessionStorage.removeItem("marketplace_access_token");
    window.sessionStorage.removeItem("marketplace_refresh_token");
    setAccessToken(""); setRefreshToken(""); setMe(null); setChallengeId(""); setOtpCode("");
    setOrders([]); setOffers([]); setProfessionalOrders([]); setProviders([]); setProfile(null);
    setBusy(false);
  }

  async function createOrder(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selectedItem || !catalog) return;
    setBusy(true); setError(""); setNotice("");
    try {
      await api("/orders", accessToken, {
        method: "POST",
        headers: { "Idempotency-Key": window.crypto.randomUUID() },
        body: JSON.stringify({
          menu_id: selectedItem.menu_id,
          scheduled_at: new Date(scheduledAt).toISOString(),
          country_code: catalog.market,
          region_code: regionCode,
          city_code: cityCode,
          area_code: areaCode,
        }),
      });
      setNotice("Your service request has been created.");
      const history = await api<{ items: OrderHistoryItem[] }>("/orders?limit=20", accessToken);
      setOrders(history.items ?? []);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Could not create the order.");
    } finally { setBusy(false); }
  }

  async function cancelOrder(orderId: string) {
    setBusy(true); setError(""); setNotice("");
    try {
      await api(`/orders/${orderId}/cancel`, accessToken, {
        method: "POST",
        headers: { "Idempotency-Key": window.crypto.randomUUID() },
        body: JSON.stringify({ reason: "Customer requested cancellation" }),
      });
      setNotice("The cancellation policy was applied. No funds were moved.");
      const history = await api<{ items: OrderHistoryItem[] }>("/orders?limit=20", accessToken);
      setOrders(history.items ?? []);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Could not cancel the order.");
    } finally { setBusy(false); }
  }

  async function registerProfessional(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true); setError(""); setNotice("");
    try {
      const result = await api<{ profile: ProviderProfile }>("/professional/profile", accessToken, {
        method: "POST", body: JSON.stringify({ display_name: profileName }),
      });
      setProfile(result.profile);
      setQualificationComplete(false);
      setNotice("Professional profile created. Add your service area and availability next.");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Could not create professional profile.");
    } finally { setBusy(false); }
  }

  async function submitQualification(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selectedItem || !catalog) return;
    setBusy(true); setError(""); setNotice("");
    try {
      await api("/professional/profile/qualification", accessToken, {
        method: "POST",
        body: JSON.stringify({
          category_id: selectedItem.category_id,
          country_code: catalog.market,
          region_code: regionCode,
          city_code: cityCode,
          area_code: areaCode,
          available_from: new Date(availabilityFrom).toISOString(),
          available_until: new Date(availabilityUntil).toISOString(),
          timezone: catalog.timezone,
        }),
      });
      setQualificationComplete(true);
      setNotice("Service area and availability submitted. An operator must verify your profile and skill before dispatch.");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Could not save professional availability.");
    } finally { setBusy(false); }
  }

  async function respondToOffer(offerId: string, action: "accept" | "decline") {
    setBusy(true); setError(""); setNotice("");
    try {
      await api(`/professional/offers/${offerId}/${action}`, accessToken, { method: "POST" });
      setNotice(action === "accept" ? "Offer accepted. The order is now assigned to you." : "Offer declined.");
      const [offerData, orderData] = await Promise.all([
        api<{ offers: Offer[] }>("/professional/offers", accessToken),
        api<{ orders: ProfessionalOrder[] }>("/professional/orders", accessToken),
      ]);
      setOffers(offerData.offers ?? []);
      setProfessionalOrders(orderData.orders ?? []);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Could not respond to offer.");
    } finally { setBusy(false); }
  }

  async function transitionProfessionalOrder(orderId: string, to: "in_progress" | "completed") {
    setBusy(true); setError(""); setNotice("");
    try {
      await api(`/professional/orders/${orderId}/transition`, accessToken, {
        method: "POST", body: JSON.stringify({ to }),
      });
      const result = await api<{ orders: ProfessionalOrder[] }>("/professional/orders", accessToken);
      setProfessionalOrders(result.orders ?? []);
      setNotice(to === "in_progress" ? "Service marked as started." : "Service marked as completed.");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Could not update order status.");
    } finally { setBusy(false); }
  }

  async function dispatchOrder(orderId: string) {
    setBusy(true); setError(""); setNotice("");
    try {
      await api(`/operator/orders/${orderId}/dispatch`, accessToken, { method: "POST" });
      const result = await api<{ orders: Order[] }>("/operator/orders", accessToken);
      setOrders(result.orders.map((order) => ({ order, created_at: "", updated_at: "" }));
      setNotice("Offers sent to eligible professionals.");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Could not dispatch the order.");
    } finally { setBusy(false); }
  }

  async function verifyProvider(provider: Provider) {
    if (!provider.category_id) {
      setError("This professional has no service category configured.");
      return;
    }
    setBusy(true); setError(""); setNotice("");
    try {
      await api(`/operator/providers/${provider.id}/verify`, accessToken, {
        method: "POST", body: JSON.stringify({ category_id: provider.category_id }),
      });
      const result = await api<{ providers: Provider[] }>("/operator/providers", accessToken);
      setProviders(result.providers ?? []);
      setNotice("Professional profile and skill verified; provider activated.");
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Could not verify professional.");
    } finally { setBusy(false); }
  }

  if (!accessToken || !me) {
    return (
      <main className="auth-shell">
        <section className="auth-card">
          <div className="brand-mark">M</div>
          <p className="eyebrow">LOCAL SERVICES, MANAGED WELL</p>
          <h1>Marketplace</h1>
          <p className="muted">Book a trusted professional, manage your service requests, or operate the marketplace.</p>
          {!challengeId ? (
            <form onSubmit={requestCode} className="form-stack">
              <label htmlFor="phone">Phone number</label>
              <input id="phone" value={phone} onChange={(event) => setPhone(event.target.value)} placeholder="+968…" autoComplete="tel" required />
              <button disabled={busy || !phone.trim()}>{busy ? "Sending…" : "Send verification code"}</button>
            </form>
          ) : (
            <form onSubmit={verifyCode} className="form-stack">
              <label htmlFor="otp">Verification code</label>
              <input id="otp" value={otpCode} onChange={(event) => setOtpCode(event.target.value)} inputMode="numeric" autoComplete="one-time-code" required />
              <button disabled={busy || !otpCode.trim()}>{busy ? "Verifying…" : "Verify and continue"}</button>
              <button type="button" className="button-secondary" onClick={() => { setChallengeId(""); setOtpCode(""); }}>Use another number</button>
            </form>
          )}
          {error && <p className="notice error">{error}</p>}
          {notice && <p className="notice success">{notice}</p>}
          <p className="footnote">Sign-in uses the configured OTP delivery provider. This MVP does not process payments.</p>
        </section>
      </main>
    );
  }

  return (
    <main className="app-shell">
      <header className="topbar">
        <div className="brand">
          <div className="brand-mark small">M</div>
          <div><strong>Marketplace</strong><span>{catalog?.market ?? "Single-market MVP"}</span></div>
        </div>
        <div className="account">
          <span>{me.phone}</span>
          <button className="button-secondary compact" onClick={signOut} disabled={busy}>Sign out</button>
        </div>
      </header>

      <section className="welcome">
        <div>
          <p className="eyebrow">SERVICE MARKETPLACE</p>
          <h1>Good service starts with trust.</h1>
          <p className="muted">Discover services, coordinate professionals, and keep every order traceable.</p>
        </div>
        <div className="market-chip"><span className="status-dot" /> {catalog?.market ?? "Loading market"}</div>
      </section>

      <nav className="role-tabs" aria-label="Workspace">
        {me.roles.map((role) => (
          <button key={role} className={activeRole === role ? "role-tab active" : "role-tab"} onClick={() => setActiveRole(role)}>
            {role === "customer" ? "Customer" : role === "professional" ? "Professional" : "Operations"}
          </button>
        ))}
      </nav>

      {error && <div className="notice error">{error}</div>}
      {notice && <div className="notice success">{notice}</div>}

      {activeRole === "customer" && (
        <div className="workspace-grid">
          <section className="panel booking-panel">
            <div className="panel-heading"><div><p className="eyebrow">NEW REQUEST</p><h2>Book a service</h2></div><span className="step-number">01</span></div>
            {catalog?.items.length ? (
              <form onSubmit={createOrder} className="form-stack">
                <label htmlFor="service">Service</label>
                <select id="service" value={selectedItem?.menu_id ?? ""} onChange={(event) => setSelectedMenu(event.target.value)}>
                  {catalog.items.map((item) => <option key={item.menu_id} value={item.menu_id}>{item.category_name} · {item.service_code} · {money(item.amount_minor,item.currency)}</option>)}
                </select>
                {selectedItem && <div className="price-line"><span>Current listed price</span><strong>{money(selectedItem.amount_minor,selectedItem.currency)}</strong></div>}
                <label htmlFor="schedule">Preferred date and time</label>
                <input id="schedule" type="datetime-local" value={scheduledAt} onChange={(event) => setScheduledAt(event.target.value)} required />
                <div className="field-grid">
                  <div><label htmlFor="region">Region</label><input id="region" value={regionCode} onChange={(event) => setRegionCode(event.target.value)} required /></div>
                  <div><label htmlFor="city">City</label><input id="city" value={cityCode} onChange={(event) => setCityCode(event.target.value)} required /></div>
                </div>
                <label htmlFor="area">Area</label>
                <input id="area" value={areaCode} onChange={(event) => setAreaCode(event.target.value)} required />
                <button disabled={busy || !selectedItem}>{busy ? "Submitting…" : "Create service request"}</button>
                <p className="footnote">The listed price is a service-price snapshot. No payment is collected in this MVP.</p>
              </form>
            ) : <p className="muted">No services are published in this market yet. Run the initial market seed procedure.</p>}
          </section>
          <section className="panel">
            <div className="panel-heading"><div><p className="eyebrow">YOUR ACTIVITY</p><h2>Recent requests</h2></div><span className="step-number">02</span></div>
            <div className="order-list">
              {orders.length ? orders.map((item) => (
                <article className="order-card" key={item.order.id}>
                  <div className="order-top"><strong>{item.order.id.slice(0,10)}</strong><span className={`status-pill status-${item.order.status}`}>{item.order.status.replaceAll("_"," ")}</span></div>
                  <p>{money(item.order.amount_minor,item.order.currency)} · {friendlyDate(item.order.scheduled_at)}</p>
                  <p className="muted small-text">{[item.order.region_code,item.order.city_code,item.order.area_code].filter(Boolean).join(" · ")}</p>
                  {["requested","accepted"].includes(item.order.status) && <button className="button-secondary compact" disabled={busy} onClick={() => cancelOrder(item.order.id)}>Cancel request</button>}
                </article>
              )) : <div className="empty-state"><span>↗</span><strong>No requests yet</strong><p>Your bookings and status updates will appear here.</p></div>}
            </div>
          </section>
        </div>
      )}

      {activeRole === "professional" && (
        <div className="workspace-grid">
          <section className="panel">
            <div className="panel-heading"><div><p className="eyebrow">PROFESSIONAL PROFILE</p><h2>Your workspace</h2></div><span className="step-number">01</span></div>
            {!profile ? (
              <form onSubmit={registerProfessional} className="form-stack">
                <p className="muted">Create a professional profile before adding your service area and availability.</p>
                <label htmlFor="display-name">Display name</label>
                <input id="display-name" value={profileName} onChange={(event) => setProfileName(event.target.value)} required />
                <button disabled={busy || !profileName.trim()}>{busy ? "Creating…" : "Create professional profile"}</button>
              </form>
            ) : (
              <div className="profile-summary">
                <div className="profile-avatar">{profile.display_name.slice(0,1).toUpperCase()}</div>
                <div><strong>{profile.display_name}</strong><p className="muted small-text">{profile.verified_at ? "Verified professional" : "Verification pending"}</p></div>
                <span className={profile.active ? "status-pill status-completed" : "status-pill status-requested"}>{profile.active ? "Active" : "Inactive"}</span>
              </div>
            )}
            {profile && !qualificationComplete && (
              <form onSubmit={submitQualification} className="form-stack section-top">
                <h3>Service area and availability</h3>
                <label htmlFor="pro-service">Service category</label>
                <select id="pro-service" value={selectedItem?.menu_id ?? ""} onChange={(event) => setSelectedMenu(event.target.value)}>
                  {catalog?.items.map((item) => <option key={item.menu_id} value={item.menu_id}>{item.category_name}</option>)}
                </select>
                <div className="field-grid">
                  <div><label htmlFor="pro-region">Region</label><input id="pro-region" value={regionCode} onChange={(event) => setRegionCode(event.target.value)} required /></div>
                  <div><label htmlFor="pro-city">City</label><input id="pro-city" value={cityCode} onChange={(event) => setCityCode(event.target.value)} required /></div>
                </div>
                <label htmlFor="pro-area">Area</label>
                <input id="pro-area" value={areaCode} onChange={(event) => setAreaCode(event.target.value)} required />
                <div className="field-grid">
                  <div><label htmlFor="available-from">Available from</label><input id="available-from" type="datetime-local" value={availabilityFrom} onChange={(event) => setAvailabilityFrom(event.target.value)} required /></div>
                  <div><label htmlFor="available-until">Available until</label><input id="available-until" type="datetime-local" value={availabilityUntil} onChange={(event) => setAvailabilityUntil(event.target.value)} required /></div>
                </div>
                <button disabled={busy || !selectedItem}>{busy ? "Saving…" : "Submit service area"}</button>
              </form>
            )}
            {profile && qualificationComplete && <p className="notice success">Your service area and availability are on file. An operator must verify your profile and skill before you can receive offers.</p>}
          </section>
          <section className="panel">
            <div className="panel-heading"><div><p className="eyebrow">DISPATCH</p><h2>Offers for you</h2></div><span className="step-number">02</span></div>
            <div className="order-list">
              {offers.length ? offers.map((offer) => (
                <article className="order-card" key={offer.id}>
                  <div className="order-top"><strong>Order {offer.order_id.slice(0,10)}</strong><span className="status-pill status-requested">Offer</span></div>
                  <p>{friendlyDate(offer.start_at)} – {friendlyDate(offer.end_at)}</p>
                  <p className="muted small-text">Expires {friendlyDate(offer.expires_at)}</p>
                  <div className="button-row">
                    <button disabled={busy} onClick={() => respondToOffer(offer.id,"accept")}>Accept</button>
                    <button className="button-secondary" disabled={busy} onClick={() => respondToOffer(offer.id,"decline")}>Decline</button>
                  </div>
                </article>
              )) : <div className="empty-state"><span>◇</span><strong>No active offers</strong><p>New eligible requests will appear here.</p></div>}
            </div>
            <div className="section-top">
              <h3>Assigned orders</h3>
              {professionalOrders.length ? professionalOrders.map((order) => (
                <article className="order-card" key={order.id}>
                  <div className="order-top"><strong>{order.id.slice(0,10)}</strong><span className={`status-pill status-${order.status}`}>{order.status.replaceAll("_"," ")}</span></div>
                  <p>{friendlyDate(order.scheduled_at)} · {money(order.amount_minor,order.currency)}</p>
                  {order.status === "accepted" && <button disabled={busy} onClick={() => transitionProfessionalOrder(order.id,"in_progress")}>Start service</button>}
                  {order.status === "in_progress" && <button disabled={busy} onClick={() => transitionProfessionalOrder(order.id,"completed")}>Complete service</button>}
                </article>
              )) : <p className="muted small-text">Accepted assignments will appear here.</p>}
            </div>
          </section>
        </div>
      )}

      {activeRole === "operator" && (
        <div className="workspace-grid">
          <section className="panel">
            <div className="panel-heading"><div><p className="eyebrow">ORDER OPERATIONS</p><h2>Market queue</h2></div><span className="step-number">01</span></div>
            <div className="order-list">
              {orders.length ? orders.map((item) => (
                <article className="order-card" key={item.order.id}>
                  <div className="order-top"><strong>{item.order.id.slice(0,10)}</strong><span className={`status-pill status-${item.order.status}`}>{item.order.status.replaceAll("_"," ")}</span></div>
                  <p>{money(item.order.amount_minor,item.order.currency)} · {friendlyDate(item.order.scheduled_at)}</p>
                  <p className="muted small-text">{[item.order.region_code,item.order.city_code,item.order.area_code].filter(Boolean).join(" · ")}</p>
                  {item.order.status === "requested" && <button disabled={busy} onClick={() => dispatchOrder(item.order.id)}>Dispatch to eligible professionals</button>}
                </article>
              )) : <div className="empty-state"><span>⌁</span><strong>Queue is clear</strong><p>New customer requests will appear here.</p></div>}
            </div>
          </section>
          <section className="panel">
            <div className="panel-heading"><div><p className="eyebrow">TRUST & SAFETY</p><h2>Professional verification</h2></div><span className="step-number">02</span></div>
            <div className="order-list">
              {providers.length ? providers.map((provider) => (
                <article className="order-card" key={provider.id}>
                  <div className="order-top"><strong>{provider.display_name}</strong><span className={provider.active && provider.verified_at ? "status-pill status-completed" : "status-pill status-requested"}>{provider.active && provider.verified_at ? "Verified" : "Pending"}</span></div>
                  <p className="muted small-text">Profile {provider.id.slice(0,10)} · Category {provider.category_id ?? "not set"}</p>
                  {provider.category_id && (!provider.verified_at || !provider.skill_verified_at || !provider.active) && <button disabled={busy} onClick={() => verifyProvider(provider)}>Verify and activate</button>}
                </article>
              )) : <div className="empty-state"><span>◎</span><strong>No professionals found</strong><p>Professional profiles appear here when they register.</p></div>}
            </div>
          </section>
        </div>
      )}

      <footer className="app-footer">
        <span>Marketplace MVP · {catalog?.market ?? "Single market"}</span>
        <span>Payments are not enabled. Cancellation policy outcomes do not move funds.</span>
      </footer>
    </main>
  );
}
