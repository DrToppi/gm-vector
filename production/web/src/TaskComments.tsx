import {usePlanningClock} from './PlanningClock';
import {tr,currentLocale} from './i18n';
import {useSessionScope} from './useSessionScope';
import {useEffect, useRef, useState, type FormEvent} from 'react';

import './comments.css';

type DeliveryState = 'internal' | 'queued' | 'sent' | 'failed' | 'uncertain';
type TaskComment = {
  id: string;
  author_name: string;
  text: string;
  created_at: string;
  send_to_customer: boolean;
  recipient: string;
  delivery_state: DeliveryState;
  delivery_error: string;
};
type CommentList = {
  comments: TaskComment[];
  email_enabled: boolean;
  can_comment: boolean;
  customer_email: string;
  customer: boolean;
};
const deliveryLabels: Record<DeliveryState, string> = {
  get internal(){return tr('m1be120293f')},
  get queued(){return tr('m14daa6e6f6')},
  get sent(){return tr('m9d2839c97a')},
  get failed(){return tr('m6f35a0ba53')},
  get uncertain(){return tr('m6ab466fbd5')},
};

const deliveryErrors: Record<string, string> = {
  get recipient_or_task_changed(){return tr('m1fffa2d3cb')},
  get task_deleted(){return tr('m874b959fe6')},
  get email_disabled(){return tr('m39e881cbc0')},
  get invalid_mail_configuration(){return tr('m6634bfae61')},
  get smtp_connection_failed(){return tr('mb31c7d4399')},
  get smtp_greeting_failed(){return tr('m7c0674d8e2')},
  get smtp_tls_required(){return tr('m3b441bc6c5')},
  get smtp_tls_failed(){return tr('m5fc2c81363')},
  get smtp_auth_failed(){return tr('ma7204a05dc')},
  get smtp_sender_rejected(){return tr('m647fdb565d')},
  get smtp_recipient_rejected(){return tr('m8ad3010704')},
  get smtp_data_rejected(){return tr('mf92050c8d2')},
  get smtp_message_rejected(){return tr('mdafae2e691')},
  get smtp_confirmation_missing(){return tr('m9db174800c')},
  get delivery_interrupted(){return tr('m6977fdc1ab')},
};

export function TaskComments({projectId, taskId, recipientChanged}: {
  projectId: string;
  taskId: string;
  recipientChanged: boolean;
}) {const clock=usePlanningClock();const scope=useSessionScope();const api=scope.api;
  const [data, setData] = useState<CommentList | null>(null);
  const [text, setText] = useState('');
  const [send, setSend] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [loadError, setLoadError] = useState('');
  const [reload, setReload] = useState(0);
  const sending = useRef(false);
  const sequence = useRef(0);
  const pending = useRef<{signature: string; id: string} | null>(null);
  const path = `/projects/${encodeURIComponent(projectId)}/tasks/${encodeURIComponent(taskId)}/comments`;
  const canSend = !!(data?.can_comment && data.email_enabled && data.customer && data.customer_email && !recipientChanged);
  const message = text.trim();
  const tooLong = new TextEncoder().encode(message).length > 8000;

  useEffect(() => {
    let mounted = true;
    let refreshing = false;
    async function refresh() {
      if (refreshing) return;
      refreshing = true;
      const version = ++sequence.current;
      try {
        const result = await api<CommentList>(path);
        if (mounted && version === sequence.current) {
          setData(result);
          setLoadError('');
        }
      } catch (cause) {
        if (mounted && version === sequence.current) setLoadError((cause as Error).message || tr('m0bbc2425fd'));
      } finally {
        refreshing = false;
      }
    }
    refresh();
    const timer = window.setInterval(refresh, 5000);
    return () => { mounted = false; window.clearInterval(timer); };
  }, [path, reload]);

  useEffect(() => { if (!canSend) setSend(false); }, [canSend]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (sending.current || !data?.can_comment || !message || tooLong || (send && !canSend)) return;
    sending.current = true;
    setBusy(true);
    setError('');
    const signature = JSON.stringify({text: message, send_to_customer: send});
    if (!pending.current || pending.current.signature !== signature) {
      pending.current = {signature, id: crypto.randomUUID()};
    }
    try {
      const comment = await api<TaskComment>(path, 'POST', {
        id: pending.current.id,
        text: message,
        send_to_customer: send,
      });
      // A GET started before this POST must not erase its confirmed result.
      sequence.current++;
      setData(current => current ? {...current, comments: [...current.comments.filter(item => item.id !== comment.id), comment]} : current);
      setText('');
      setSend(false);
      pending.current = null;
    } catch (cause) {
      // Keep both the draft and UUID for an idempotent retry after network loss.
      setError((cause as Error).message || tr('m392211886a'));
    } finally {
      sending.current = false;
      setBusy(false);
    }
  }

  return <section className="task-comments" aria-label={tr('m869e7279dd')} data-task-comments={taskId}>
    <h3>{tr('m497d1e5000')}</h3>
    <p className="muted">{tr('mc2d78bccb4')}</p>
    {!data && !loadError && <p role="status">{tr('m34727785a8')}</p>}
    {loadError && <div className="error" role="alert">{loadError}{!data && <button type="button" className="secondary" onClick={() => setReload(value => value + 1)}>{tr('m298e41327f')}</button>}</div>}
    {error && <div className="error" role="alert">{error}</div>}
    {data && <>
      <div className="task-comments-list" aria-label={tr('mfe53a02758')}>
        {data.comments.length === 0 && <p className="muted">{tr('m3247cd32ea')}</p>}
        {data.comments.map(comment => <article key={comment.id} data-comment-id={comment.id}>
          <div className="task-comment-heading"><strong>{comment.author_name}</strong><time dateTime={comment.created_at}>{clock.timestamp(comment.created_at)}</time></div>
          <p className="task-comment-text">{comment.text}</p>
          <div className="task-comment-delivery" data-delivery-state={comment.delivery_state}>
            <span>{deliveryLabels[comment.delivery_state] || tr('m293e95311f')}</span>
            {comment.send_to_customer && comment.recipient && <span> · {comment.recipient}</span>}
          </div>
          {comment.delivery_error && <p className="task-comment-error">{deliveryErrors[comment.delivery_error] || tr('m5dd749f16a')}</p>}
          {comment.delivery_state === 'uncertain' && <p className="muted">{tr('mce8c7aaf8a')}</p>}
        </article>)}
      </div>
      {data.can_comment ? <form className="task-comment-form" onSubmit={submit}>
        <label>{tr('mc62d2ef7a0')}<textarea aria-label={tr('mc62d2ef7a0')} value={text} onChange={event => setText(event.target.value)} maxLength={8000} rows={4} disabled={busy} required/></label>
        {tooLong && <p className="error" role="alert">{tr('mac48bd37e0')}</p>}
        <label className="check"><input type="checkbox" aria-label={tr('m38e51dc8af')} checked={send} onChange={event => setSend(event.target.checked)} disabled={busy || !canSend}/>{tr('m38e51dc8af')}</label>
        {data.customer && data.customer_email && <p className="task-comment-recipient">{tr('m4569e9478c')} <strong>{data.customer_email}</strong></p>}
        {recipientChanged && <p className="muted">{tr('mf660862309')}</p>}
        {!data.email_enabled && <p className="muted" data-email-disabled>{tr('mf63617c242')}</p>}
        {data.email_enabled && !recipientChanged && (!data.customer || !data.customer_email) && <p className="muted">{tr('ma6eae2e96f')}</p>}
        <div className="task-comment-actions"><button type="submit" disabled={busy || !message || tooLong || (send && !canSend)}>{busy ? tr('m9c27c381dd') : send ? tr('mda370affd8') : tr('m3505110de9')}</button></div>
      </form> : <p className="muted">{tr('m83e791ac48')}</p>}
    </>}
  </section>;
}
