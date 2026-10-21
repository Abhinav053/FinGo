import { FormEvent, useCallback, useEffect, useState } from 'react';

type Payment = {
  id: string;
  amount: number;
  currency: string;
  status: 'CREATED' | 'PENDING' | 'SUCCEEDED' | 'FAILED' | 'UNKNOWN';
  description: string;
  failure_reason: string | null;
  created_at: string;
};

type PaymentList = {
  data: Payment[];
  total: number;
};

const API_BASE = (import.meta.env.VITE_API_BASE_URL ?? 'http://localhost:3000/api').replace(/\/$/, '');

async function api<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers: { 'Content-Type': 'application/json', ...options?.headers },
  });
  if (!response.ok) {
    const problem = (await response.json().catch(() => null)) as { message?: string } | null;
    throw new Error(problem?.message ?? `Request failed (${response.status})`);
  }
  return response.json() as Promise<T>;
}

function money(amount: number, currency: string) {
  return new Intl.NumberFormat(undefined, { style: 'currency', currency }).format(amount / 100);
}

export default function App() {
  const [payments, setPayments] = useState<Payment[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const refresh = useCallback(async () => {
    try {
      const result = await api<PaymentList>('/v1/payments?limit=100');
      setPayments(result.data);
      setTotal(result.total);
      setError('');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unable to load payments');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
    const timer = window.setInterval(() => void refresh(), 5000);
    return () => window.clearInterval(timer);
  }, [refresh]);

  async function createPayment(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const formElement = event.currentTarget;
    const form = new FormData(formElement);
    const amountRupees = Number(form.get('amount'));
    if (!Number.isFinite(amountRupees) || amountRupees <= 0) {
      setError('Enter an amount greater than zero.');
      return;
    }
    setBusy(true);
    try {
      await api('/v1/payments', {
        method: 'POST',
        headers: { 'Idempotency-Key': crypto.randomUUID() },
        body: JSON.stringify({
          amount: Math.round(amountRupees * 100),
          currency: String(form.get('currency')).toUpperCase(),
          description: String(form.get('description') ?? '').trim(),
        }),
      });
      formElement.reset();
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unable to create payment');
    } finally {
      setBusy(false);
    }
  }

  async function refundPayment(payment: Payment) {
    const rupees = window.prompt(`Refund amount (up to ${money(payment.amount, payment.currency)}):`, (payment.amount / 100).toFixed(2));
    if (rupees === null) return;
    const amount = Math.round(Number(rupees) * 100);
    if (!Number.isSafeInteger(amount) || amount < 1) {
      setError('Refund amount must be a positive amount with at most two decimal places.');
      return;
    }
    setBusy(true);
    try {
      await api(`/v1/payments/${encodeURIComponent(payment.id)}/refunds`, {
        method: 'POST',
        body: JSON.stringify({ amount, reason: 'Dashboard refund' }),
      });
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unable to create refund');
    } finally {
      setBusy(false);
    }
  }

  const succeeded = payments.filter((payment) => payment.status === 'SUCCEEDED').length;
  const volume = payments
    .filter((payment) => payment.status === 'SUCCEEDED' && payment.currency === 'INR')
    .reduce((sum, payment) => sum + payment.amount, 0);

  return (
    <main className="shell">
      <header className="topbar">
        <a className="brand" href="/" aria-label="FinGo home"><span className="brand-mark">F</span>FinGo</a>
        <div className="live"><span /> Payments workspace</div>
      </header>

      <section className="intro">
        <div>
          <p className="eyebrow">PAYMENT OPERATIONS</p>
          <h1>Good things happen<br />when payments flow.</h1>
          <p className="subtitle">Create, monitor, and manage payment activity from one place.</p>
        </div>
        <form className="create-card" onSubmit={createPayment}>
          <div className="card-heading">
            <div><p className="eyebrow">GET STARTED</p><h2>Create a payment</h2></div>
            <span className="plus">+</span>
          </div>
          <label>
            Amount
            <div className="amount-input"><span>₹</span><input name="amount" type="number" min="0.01" step="0.01" placeholder="1,000.00" required /></div>
          </label>
          <div className="form-row">
            <label>Currency<select name="currency" defaultValue="INR"><option>INR</option><option>USD</option><option>EUR</option><option>GBP</option></select></label>
            <label>Description<input name="description" maxLength={500} placeholder="Order or invoice" /></label>
          </div>
          <button className="primary-button" disabled={busy}>{busy ? 'Processing…' : 'Create payment'} <span>→</span></button>
        </form>
      </section>

      <section className="metrics" aria-label="Payment summary">
        <article className="metric"><span>Total payments</span><strong>{total}</strong><small>All payment attempts</small></article>
        <article className="metric"><span>Successful</span><strong>{succeeded}</strong><small>Payments completed</small></article>
        <article className="metric"><span>INR volume</span><strong>{money(volume, 'INR')}</strong><small>Successfully processed</small></article>
      </section>

      <section className="payments-section">
        <div className="section-heading"><div><p className="eyebrow">ACTIVITY</p><h2>Recent payments</h2></div><button className="refresh-button" onClick={() => void refresh()} disabled={loading}>↻ Refresh</button></div>
        {error && <div className="alert" role="alert">{error}<button onClick={() => setError('')} aria-label="Dismiss error">×</button></div>}
        {loading ? <div className="empty">Loading payments…</div> : payments.length === 0 ? (
          <div className="empty"><span className="empty-icon">↗</span><strong>No payments yet</strong><p>Create a payment to see its lifecycle here.</p></div>
        ) : (
          <div className="table-wrap"><table>
            <thead><tr><th>PAYMENT</th><th>AMOUNT</th><th>STATUS</th><th>CREATED</th><th /></tr></thead>
            <tbody>{payments.map((payment) => (
              <tr key={payment.id}>
                <td><strong>{payment.description || 'Payment'}</strong><small>{payment.id}</small>{payment.failure_reason && <small className="failure">{payment.failure_reason}</small>}</td>
                <td className="amount">{money(payment.amount, payment.currency)}</td>
                <td><span className={`status status-${payment.status.toLowerCase()}`}><i />{payment.status}</span></td>
                <td className="date">{new Date(payment.created_at).toLocaleString()}</td>
                <td>{payment.status === 'SUCCEEDED' && <button className="text-button" onClick={() => void refundPayment(payment)} disabled={busy}>Refund</button>}</td>
              </tr>
            ))}</tbody>
          </table></div>
        )}
      </section>
      <footer>FinGo · Payment processing demo <span>Amounts are stored and processed in minor currency units.</span></footer>
    </main>
  );
}
