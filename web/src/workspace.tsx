import {
  Alert, Button, Checkbox, CircularProgress, FormControl, FormControlLabel,
  InputLabel, MenuItem, Select, TextField,
} from '@mui/material';
import { FormEvent, useEffect, useState } from 'react';
import { useParams } from 'react-router';

type Identity = { id: string; businessId: string; businessName: string; email: string };
type Category = { id: string; name: string };
type Product = { id: string; categoryId: string | null; name: string; description: string; price: string; currency: string; available: boolean };
type Client = { id: string; displayName: string; email?: string; phone?: string };
type InvoiceItem = { productId?: string; productName: string; description: string; quantity: number; unitPrice: string; lineTotal: string };
type Invoice = { id: string; reference: string; clientId: string; clientName: string; currency: string; subtotal: string; total: string; status: string; createdAt: string; items?: InvoiceItem[]; publicUrl?: string };
type PublicInvoice = { reference: string; businessName: string; currency: string; subtotal: string; total: string; status: string; createdAt: string; expiresAt: string; items: InvoiceItem[] };
type Section = 'catalog' | 'clients' | 'invoices';

async function request<T>(path: string, csrfToken: string, init: RequestInit = {}): Promise<T> {
  const method = (init.method ?? 'GET').toUpperCase();
  const headers = new Headers(init.headers);
  if (method !== 'GET' && method !== 'HEAD') headers.set('X-CSRF-Token', csrfToken);
  if (init.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
  const response = await fetch(path, { ...init, headers, credentials: 'same-origin' });
  const result = response.status === 204 ? undefined : await response.json().catch(() => undefined);
if (!response.ok) throw new Error((result as { error?: string } | undefined)?.error ?? 'The request could not be completed.');
  return result as T;
}

export function WorkspacePage({ identity, csrfToken, onLogout }: { identity: Identity; csrfToken: string; onLogout: () => Promise<void> }) {
  const [section, setSection] = useState<Section>('catalog');
  const [categories, setCategories] = useState<Category[]>([]);
  const [products, setProducts] = useState<Product[]>([]);
  const [clients, setClients] = useState<Client[]>([]);
  const [invoices, setInvoices] = useState<Invoice[]>([]);
  const [selectedInvoice, setSelectedInvoice] = useState<Invoice | null>(null);
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  async function refresh() {
    setError('');
    setBusy(true);
    try {
      const [nextCategories, nextProducts, nextClients, nextInvoices] = await Promise.all([
        request<Category[]>('/api/catalog/categories', csrfToken),
        request<Product[]>('/api/catalog/products', csrfToken),
        request<Client[]>('/api/clients', csrfToken),
        request<Invoice[]>('/api/invoices', csrfToken),
      ]);
      setCategories(nextCategories);
      setProducts(nextProducts);
      setClients(nextClients);
      setInvoices(nextInvoices);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Workspace data could not be loaded.');
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => { void refresh(); }, [csrfToken]);

  async function save(path: string, method: string, body: unknown, success: string) {
    setError('');
    setNotice('');
    setBusy(true);
    try {
      await request(path, csrfToken, { method, body: JSON.stringify(body) });
      setNotice(success);
      await refresh();
      return true;
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'The change could not be saved.');
      return false;
    } finally {
      setBusy(false);
    }
  }

  async function openInvoice(invoice: Invoice) {
    setError('');
    try {
      setSelectedInvoice(await request<Invoice>(`/api/invoices/${invoice.id}`, csrfToken));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Invoice could not be loaded.');
    }
  }

  async function createLink(invoice: Invoice) {
    setError('');
    try {
      const result = await request<{ url: string }>(`/api/invoices/${invoice.id}/public-link`, csrfToken, { method: 'POST', body: '{}' });
      setSelectedInvoice({ ...invoice, publicUrl: result.url });
      setNotice('Public link created. The previous invoice link, if any, has been revoked.');
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Public link could not be created.');
    }
  }

  async function copyLink(url: string) {
    try {
      await navigator.clipboard.writeText(new URL(url, window.location.origin).toString());
      setNotice('Invoice link copied.');
    } catch {
      setNotice(`Copy this invoice link: ${new URL(url, window.location.origin).toString()}`);
    }
  }

  return (
    <main className="workspace-page">
      <header className="topbar">
        <a className="wordmark" href="/">WispPay<span> / WORKSPACE</span></a>
        <div className="account-menu"><span>{identity.email}</span><Button size="small" variant="outlined" onClick={() => void onLogout()} disabled={busy}>Sign out</Button></div>
      </header>
      <section className="workspace-heading">
        <p className="eyebrow">BUSINESS WORKSPACE</p>
        <h1>{identity.businessName}</h1>
      </section>
      <PayPalConnectPanel csrfToken={csrfToken} />
      <nav className="workspace-nav" aria-label="Workspace sections">
        {(['catalog', 'clients', 'invoices'] as Section[]).map((item) => (
          <Button key={item} onClick={() => { setSection(item); setSelectedInvoice(null); setNotice(''); setError(''); }} variant={section === item ? 'contained' : 'text'}>
            {item === 'catalog' ? 'Catalog' : item[0].toUpperCase() + item.slice(1)}
          </Button>
        ))}
        <Button onClick={() => void refresh()} disabled={busy}>Refresh</Button>
      </nav>
      <div className="workspace-content">
        {error && <Alert severity="error" onClose={() => setError('')}>{error}</Alert>}
        {notice && <Alert severity="success" onClose={() => setNotice('')}>{notice}</Alert>}
        {busy && <div className="loading-line"><CircularProgress size={18} /> Loading workspace</div>}
        {section === 'catalog' && <CatalogSection categories={categories} products={products} busy={busy} save={save} />}
        {section === 'clients' && <ClientsSection clients={clients} busy={busy} save={save} />}
        {section === 'invoices' && <InvoicesSection clients={clients} products={products} invoices={invoices} selected={selectedInvoice} busy={busy} openInvoice={openInvoice} createLink={createLink} copyLink={copyLink} refresh={refresh} setNotice={setNotice} setError={setError} onLinkRevoked={() => setSelectedInvoice((current) => current ? { ...current, publicUrl: undefined } : current)} csrfToken={csrfToken} />}
      </div>
      <footer>WispPay <span>Secure invoicing and payment links</span></footer>
    </main>
  );
}

function CatalogSection({ categories, products, busy, save }: { categories: Category[]; products: Product[]; busy: boolean; save: WorkspacePageProps['save'] }) {
  const [categoryName, setCategoryName] = useState('');
  const [categoryEdit, setCategoryEdit] = useState('');
  const [productEdit, setProductEdit] = useState<Product | null>(null);
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [price, setPrice] = useState('');
  const [currency, setCurrency] = useState('USD');
  const [categoryId, setCategoryId] = useState('');
  const [available, setAvailable] = useState(true);

  function editProduct(product: Product) {
    setProductEdit(product); setName(product.name); setDescription(product.description); setPrice(product.price);
    setCurrency(product.currency); setCategoryId(product.categoryId ?? ''); setAvailable(product.available);
  }
  async function submitCategory(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const path = categoryEdit ? `/api/catalog/categories/${categoryEdit}` : '/api/catalog/categories';
    if (await save(path, categoryEdit ? 'PATCH' : 'POST', { name: categoryName }, categoryEdit ? 'Category updated.' : 'Category created.')) {
      setCategoryName(''); setCategoryEdit('');
    }
  }
  async function submitProduct(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const body = { name, description, price, currency, categoryId: categoryId || null, available };
    const path = productEdit ? `/api/catalog/products/${productEdit.id}` : '/api/catalog/products';
    if (await save(path, productEdit ? 'PATCH' : 'POST', body, productEdit ? 'Product updated.' : 'Product created.')) {
      setProductEdit(null); setName(''); setDescription(''); setPrice(''); setCategoryId(''); setAvailable(true);
    }
  }

  return <>
    <section className="workspace-section">
      <div className="section-title"><div><p className="eyebrow">ORGANIZE</p><h2>Categories</h2></div></div>
      <form className="inline-form" onSubmit={submitCategory}>
        <TextField size="small" label="Category name" value={categoryName} onChange={(event) => setCategoryName(event.target.value)} required slotProps={{ htmlInput: { maxLength: 80 } }} />
        <Button type="submit" variant="contained" disabled={busy}>{categoryEdit ? 'Save category' : 'Add category'}</Button>
        {categoryEdit && <Button type="button" onClick={() => { setCategoryEdit(''); setCategoryName(''); }}>Cancel</Button>}
      </form>
      {categories.length === 0 ? <p className="empty-state">No categories yet. Categories are optional for products.</p> : <div className="record-list">{categories.map((category) => <div className="record-row" key={category.id}><strong>{category.name}</strong><Button size="small" onClick={() => { setCategoryEdit(category.id); setCategoryName(category.name); }}>Rename</Button></div>)}</div>}
    </section>
    <section className="workspace-section">
      <div className="section-title"><div><p className="eyebrow">YOUR OFFERING</p><h2>{productEdit ? 'Edit product' : 'Products'}</h2></div></div>
      <form className="data-form" onSubmit={submitProduct}>
        <TextField label="Product name" value={name} onChange={(event) => setName(event.target.value)} required slotProps={{ htmlInput: { maxLength: 120 } }} />
        <TextField label="Description" value={description} onChange={(event) => setDescription(event.target.value)} multiline minRows={2} slotProps={{ htmlInput: { maxLength: 2000 } }} />
        <div className="form-pair">
          <TextField label="Price" value={price} onChange={(event) => setPrice(event.target.value)} required slotProps={{ htmlInput: { inputMode: 'decimal', pattern: '\\d+(\\.\\d{1,2})?' } }} />
          <TextField label="Currency" value={currency} onChange={(event) => setCurrency(event.target.value.toUpperCase())} required slotProps={{ htmlInput: { maxLength: 3 } }} />
        </div>
        <FormControl><InputLabel id="category-select-label">Category</InputLabel><Select labelId="category-select-label" label="Category" value={categoryId} onChange={(event) => setCategoryId(event.target.value)}><MenuItem value="">Uncategorized</MenuItem>{categories.map((category) => <MenuItem key={category.id} value={category.id}>{category.name}</MenuItem>)}</Select></FormControl>
        <FormControlLabel control={<Checkbox checked={available} onChange={(event) => setAvailable(event.target.checked)} />} label="Available for new invoices" />
        <div className="form-actions"><Button type="submit" variant="contained" disabled={busy}>{productEdit ? 'Save product' : 'Add product'}</Button>{productEdit && <Button type="button" onClick={() => { setProductEdit(null); setName(''); setDescription(''); setPrice(''); }}>Cancel edit</Button>}</div>
      </form>
      <h3 className="list-heading">Catalog</h3>
      {products.length === 0 ? <p className="empty-state">Your catalog is empty.</p> : <div className="record-list">{products.map((product) => <div className="record-row product-row" key={product.id}><div><strong>{product.name}</strong><span>{product.description || 'No description'}</span></div><strong>{product.currency} {product.price}</strong><span>{product.available ? 'Available' : 'Unavailable'}</span><Button size="small" onClick={() => editProduct(product)}>Edit</Button></div>)}</div>}
    </section>
  </>;
}

function ClientsSection({ clients, busy, save }: { clients: Client[]; busy: boolean; save: WorkspacePageProps['save'] }) {
  const [editing, setEditing] = useState<Client | null>(null);
  const [displayName, setDisplayName] = useState('');
  const [email, setEmail] = useState('');
  const [phone, setPhone] = useState('');
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const body = { displayName, email, phone };
    const path = editing ? `/api/clients/${editing.id}` : '/api/clients';
    if (await save(path, editing ? 'PATCH' : 'POST', body, editing ? 'Client updated.' : 'Client added.')) {
      setEditing(null); setDisplayName(''); setEmail(''); setPhone('');
    }
  }
  return <>
    <section className="workspace-section">
      <div className="section-title"><div><p className="eyebrow">CUSTOMERS</p><h2>{editing ? 'Edit client' : 'Clients'}</h2></div></div>
      <form className="data-form" onSubmit={submit}>
        <TextField label="Display name" value={displayName} onChange={(event) => setDisplayName(event.target.value)} required slotProps={{ htmlInput: { minLength: 2, maxLength: 120 } }} />
        <TextField label="Email (optional)" type="email" value={email} onChange={(event) => setEmail(event.target.value)} slotProps={{ htmlInput: { maxLength: 254 } }} />
        <TextField label="Phone (optional)" value={phone} onChange={(event) => setPhone(event.target.value)} slotProps={{ htmlInput: { maxLength: 32 } }} />
        <div className="form-actions"><Button type="submit" variant="contained" disabled={busy}>{editing ? 'Save client' : 'Add client'}</Button>{editing && <Button type="button" onClick={() => { setEditing(null); setDisplayName(''); setEmail(''); setPhone(''); }}>Cancel</Button>}</div>
      </form>
    </section>
    <section className="workspace-section"><h3 className="list-heading">Client list</h3>
      {clients.length === 0 ? <p className="empty-state">No clients yet. Add a client before creating an invoice.</p> : <div className="record-list">{clients.map((client) => <div className="record-row" key={client.id}><div><strong>{client.displayName}</strong><span>{[client.email, client.phone].filter(Boolean).join(' · ') || 'No contact details'}</span></div><Button size="small" onClick={() => { setEditing(client); setDisplayName(client.displayName); setEmail(client.email ?? ''); setPhone(client.phone ?? ''); }}>Edit</Button></div>)}</div>}
    </section>
  </>;
}

function InvoicesSection({ clients, products, invoices, selected, busy, openInvoice, createLink, copyLink, refresh, setNotice, setError, onLinkRevoked, csrfToken }: {
  clients: Client[]; products: Product[]; invoices: Invoice[]; selected: Invoice | null; busy: boolean;
  openInvoice: (invoice: Invoice) => Promise<void>; createLink: (invoice: Invoice) => Promise<void>; onLinkRevoked: () => void;
  copyLink: (url: string) => Promise<void>; refresh: () => Promise<void>; setNotice: (notice: string) => void; setError: (error: string) => void; csrfToken: string;
}) {
  const [clientId, setClientId] = useState('');
  const [quantities, setQuantities] = useState<Record<string, number>>({});
  async function createInvoice(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const items = Object.entries(quantities).filter(([, quantity]) => quantity > 0).map(([productId, quantity]) => ({ productId, quantity }));
    if (!clientId || items.length === 0) { setError('Choose a client and at least one available product.'); return; }
    setError('');
    try {
      const invoice = await request<Invoice>('/api/invoices', csrfToken, { method: 'POST', body: JSON.stringify({ clientId, items }) });
      await refresh();
      await openInvoice(invoice);
      setNotice('Invoice created with server-calculated totals.');
      setClientId(''); setQuantities({});
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Invoice could not be created.'); }
  }
  async function revokeLink() {
    if (!selected) return;
    try {
      await request(`/api/invoices/${selected.id}/public-link`, csrfToken, { method: 'DELETE' });
      setNotice('Public invoice link revoked.');
      onLinkRevoked();
    } catch (cause) { setError(cause instanceof Error ? cause.message : 'Link could not be revoked.'); }
  }

  return <>
    <section className="workspace-section">
      <div className="section-title"><div><p className="eyebrow">BILLING</p><h2>Create invoice</h2></div></div>
      {clients.length === 0 || products.filter((product) => product.available).length === 0 ? <Alert severity="info">Add a client and at least one available product before creating an invoice.</Alert> : <form className="data-form" onSubmit={createInvoice}>
        <FormControl><InputLabel id="invoice-client-label">Client</InputLabel><Select labelId="invoice-client-label" label="Client" value={clientId} onChange={(event) => setClientId(event.target.value)} required><MenuItem value="" disabled>Select a client</MenuItem>{clients.map((client) => <MenuItem key={client.id} value={client.id}>{client.displayName}</MenuItem>)}</Select></FormControl>
        <div className="invoice-product-picks">{products.filter((product) => product.available).map((product) => <div className="pick-row" key={product.id}><FormControlLabel control={<Checkbox checked={(quantities[product.id] ?? 0) > 0} onChange={(event) => setQuantities((current) => ({ ...current, [product.id]: event.target.checked ? 1 : 0 }))} />} label={`${product.name} · ${product.currency} ${product.price}`} /><TextField label="Qty" type="number" size="small" disabled={!quantities[product.id]} value={quantities[product.id] || ''} onChange={(event) => setQuantities((current) => ({ ...current, [product.id]: Number(event.target.value) }))} slotProps={{ htmlInput: { min: 1, max: 10000, step: 1 } }} /></div>)}</div>
        <Button type="submit" variant="contained" disabled={busy}>Create invoice</Button>
      </form>}
    </section>
    <section className="workspace-section"><div className="section-title"><div><p className="eyebrow">HISTORY</p><h2>Invoices</h2></div></div>
      {invoices.length === 0 ? <p className="empty-state">No invoices have been created.</p> : <div className="record-list">{invoices.map((invoice) => <div className="record-row invoice-row" key={invoice.id}><div><strong>{invoice.reference}</strong><span>{invoice.clientName} · {new Date(invoice.createdAt).toLocaleDateString()}</span></div><strong>{invoice.currency} {invoice.total}</strong><span className="status-text">{invoice.status}</span><Button size="small" onClick={() => void openInvoice(invoice)}>Details</Button></div>)}</div>}
    </section>
    {selected && <section className="workspace-section invoice-detail" aria-labelledby="invoice-detail-title">
      <div className="section-title"><div><p className="eyebrow">INVOICE DETAIL</p><h2 id="invoice-detail-title">{selected.reference}</h2></div><Button onClick={() => void openInvoice(selected)} disabled={busy}>Reload</Button></div>
      <div className="invoice-summary"><span>Client<strong>{selected.clientName}</strong></span><span>Status<strong>{selected.status}</strong></span><span>Total<strong>{selected.currency} {selected.total}</strong></span></div>
      <ItemRows items={selected.items ?? []} currency={selected.currency} />
      <div className="link-actions"><Button variant="contained" onClick={() => void createLink(selected)}>Create new public link</Button>{selected.publicUrl && <Button onClick={() => void copyLink(selected.publicUrl!)}>Copy link</Button>}{selected.publicUrl && <Button color="error" onClick={() => void revokeLink()}>Revoke current link</Button>}</div>
    </section>}
  </>;
}

type WorkspacePageProps = { save: (path: string, method: string, body: unknown, success: string) => Promise<boolean> };

function ItemRows({ items, currency }: { items: InvoiceItem[]; currency: string }) {
  return <div className="record-list invoice-items">{items.map((item, index) => <div className="record-row" key={`${item.productId ?? item.productName}-${index}`}><div><strong>{item.productName}</strong><span>{item.description}</span></div><span>{item.quantity} × {currency} {item.unitPrice}</span><strong>{currency} {item.lineTotal}</strong></div>)}</div>;
}

function PayPalConnectPanel({ csrfToken }: { csrfToken: string }) {
  const [status, setStatus] = useState('loading');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  async function refresh() {
    try {
      const result = await request<{ status: string; eligible: boolean }>('/api/paypal/seller', csrfToken);
      setStatus(result.eligible ? 'active' : result.status);
    } catch (cause) {
      setStatus('unavailable');
      setError(cause instanceof Error ? cause.message : 'PayPal connection status could not be loaded.');
    }
  }

  useEffect(() => { void refresh(); }, [csrfToken]);

  async function connect() {
    setBusy(true);
    setError('');
    try {
      const result = await request<{ url: string }>('/api/paypal/seller/onboarding', csrfToken, { method: 'POST', body: '{}' });
      window.location.assign(result.url);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'PayPal connection could not be started.');
      setBusy(false);
    }
  }

  return <section className="workspace-section payment-connection" aria-labelledby="paypal-connection-title">
    <div className="section-title"><div><p className="eyebrow">CONNECTED SELLER</p><h2 id="paypal-connection-title">PayPal payments</h2></div>
      {status === 'active' ? <span className="status-text">Connected and eligible</span> : status === 'pending' ? <span className="status-text">Setup incomplete</span> : null}
    </div>
    {error && <Alert severity={status === 'unavailable' ? 'info' : 'error'} onClose={() => setError('')}>{status === 'unavailable' ? 'PayPal seller payments are not configured for this deployment.' : error}</Alert>}
    {status === 'active' ? <p className="empty-state">Your business receives invoice payments through its connected PayPal account.</p> : status === 'unavailable' ? null : <>
      {status === 'pending' && <p className="empty-state">PayPal has not confirmed all permissions and payment eligibility. Complete setup or reconnect to check again.</p>}
      <Button variant="outlined" onClick={() => void connect()} disabled={busy || status === 'loading'}>{busy ? 'Connecting…' : status === 'pending' ? 'Continue PayPal setup' : 'Connect PayPal'}</Button>
    </>}
  </section>;
}

export function PublicInvoicePage() {
  const { token = '' } = useParams();
  const [invoice, setInvoice] = useState<PublicInvoice | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [paymentAvailable, setPaymentAvailable] = useState(false);
  const [paymentBusy, setPaymentBusy] = useState(false);
  const [paymentMessage, setPaymentMessage] = useState('');
  useEffect(() => {
    let active = true;
    async function load() {
      const query = new URLSearchParams(window.location.search);
      const orderId = query.get('token') ?? '';
      const returnedFromApproval = query.get('payment') === 'approved' && orderId !== '';
      if (returnedFromApproval) {
        setPaymentBusy(true);
        setPaymentMessage('Confirming payment with PayPal…');
        try {
          await request(`/api/public/invoices/${encodeURIComponent(token)}/paypal/capture`, '', {
            method: 'POST', body: JSON.stringify({ orderId }),
          });
          if (active) setPaymentMessage('Payment completed.');
        } catch (cause) {
          if (active) setPaymentMessage(cause instanceof Error ? cause.message : 'Payment could not be confirmed.');
        } finally {
          if (active) {
            setPaymentBusy(false);
            window.history.replaceState({}, '', window.location.pathname);
          }
        }
      } else if (query.get('payment') === 'cancelled' && active) {
        setPaymentMessage('PayPal checkout was cancelled. No payment was recorded.');
        window.history.replaceState({}, '', window.location.pathname);
      }
      try {
        const result = await request<PublicInvoice>(`/api/public/invoices/${encodeURIComponent(token)}`, '');
        if (active) setInvoice(result);
      } catch (cause) {
        if (active) {
          setError(cause instanceof Error ? cause.message : 'Invoice link is unavailable.');
          setLoading(false);
        }
        return;
      }
      try {
        const capability = await request<{ available: boolean }>(`/api/public/invoices/${encodeURIComponent(token)}/paypal`, '');
        if (active) setPaymentAvailable(capability.available);
      } catch {
        if (active) setPaymentAvailable(false);
      } finally {
        if (active) setLoading(false);
      }
    }
    void load();
    return () => { active = false; };
  }, [token]);

  async function startPayment() {
    setPaymentBusy(true);
    setPaymentMessage('');
    try {
      const result = await request<{ orderId: string; approvalUrl: string }>(`/api/public/invoices/${encodeURIComponent(token)}/paypal/orders`, '', {
        method: 'POST', body: '{}',
      });
      window.location.assign(result.approvalUrl);
    } catch (cause) {
      setPaymentMessage(cause instanceof Error ? cause.message : 'PayPal checkout could not be started.');
      setPaymentBusy(false);
    }
  }

  return <main className="workspace-page public-page">
    <header className="topbar"><a className="wordmark" href="/">WispPay<span> / INVOICE</span></a></header>
    <section className="workspace-section public-invoice">
      {loading ? <div className="loading-line"><CircularProgress size={18} /> Loading invoice</div> : error ? <Alert severity="error">{error}</Alert> : invoice && <>
        <p className="eyebrow">INVOICE FROM {invoice.businessName.toUpperCase()}</p>
        <div className="section-title"><div><h1>{invoice.reference}</h1><p className="intro-copy">Issued {new Date(invoice.createdAt).toLocaleDateString()}</p></div><span className="status-text">{invoice.status}</span></div>
        <ItemRows items={invoice.items} currency={invoice.currency} />
        <div className="public-total"><span>Total due</span><strong>{invoice.currency} {invoice.total}</strong></div>
        {paymentMessage && <Alert severity={paymentMessage.includes('completed') ? 'success' : paymentMessage.includes('Confirming') ? 'info' : 'warning'}>{paymentMessage}</Alert>}
        {paymentAvailable && invoice.status === 'open' && <Button variant="contained" onClick={() => void startPayment()} disabled={paymentBusy}>{paymentBusy ? <CircularProgress size={18} color="inherit" /> : 'Pay with PayPal'}</Button>}
        {!paymentAvailable && invoice.status === 'open' && <Alert severity="info">Online payment is unavailable for this invoice. Contact the business directly to arrange payment.</Alert>}
        <p className="expiry-note">This invoice link expires {new Date(invoice.expiresAt).toLocaleDateString()}.</p>
      </>}
    </section>
    <footer>WispPay <span>Secure invoicing and payment links</span></footer>
  </main>;
}
