import React, { useEffect, useState } from 'react';
import type { PendingApproval, ResolvedLang } from '../types';
import { translate } from '../lib/i18n';

interface ApprovalCardProps {
  approval: PendingApproval;
  lang: ResolvedLang;
  onReply: (
    id: string,
    decision: 'allow_once' | 'allow_always' | 'reject',
    optionId?: string,
    reason?: string
  ) => Promise<void>;
}

export const ApprovalCard: React.FC<ApprovalCardProps> = ({ approval, lang, onReply }) => {
  const t = (k: string, vars?: Record<string, string | number>) => translate(lang, k, vars);
  const [reason, setReason] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [err, setErr] = useState('');

  useEffect(() => {
    setReason('');
    setSubmitting(false);
    setErr('');
  }, [approval.id]);

  const kindKey = `permKind_${approval.kind || 'other'}`;
  const kindLabel = translate(lang, kindKey) === kindKey ? approval.kind || 'tool' : translate(lang, kindKey);

  const opts = approval.options || [];
  const allowOnceOpt = opts.find((o) => o.kind === 'allow_once');
  const allowAlwaysOpt = opts.find((o) => o.kind === 'allow_always');
  const rejectOpt = opts.find((o) => o.kind === 'reject');

  const hasRawInput = Boolean(approval.rawInput && Object.keys(approval.rawInput).length > 0);
  const showTitle = Boolean(approval.title && approval.title !== approval.toolName);

  const submit = async (
    decision: 'allow_once' | 'allow_always' | 'reject',
    optionId?: string,
    denyReason?: string
  ) => {
    if (submitting) return;
    setSubmitting(true);
    setErr('');
    try {
      await onReply(approval.id, decision, optionId, denyReason);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="q-card apr-card fresh" role="region" aria-label={t('approvalTitle')}>
      <div className="q-hd">
        <div className="apr-hd-left">
          <span className={`apr-kind kind-${approval.kind || 'other'}`}>{kindLabel}</span>
          <span className="apr-tool mono">{approval.toolName}</span>
        </div>
        <span className="q-badge">{t('approvalWaitingBadge')}</span>
      </div>

      {showTitle && <div className="apr-title">{approval.title}</div>}

      {approval.summary && (
        <pre className="apr-summary mono">{approval.summary}</pre>
      )}

      {hasRawInput && (
        <details className="apr-raw">
          <summary>{t('approvalParams')}</summary>
          <pre className="mono">{JSON.stringify(approval.rawInput, null, 2)}</pre>
        </details>
      )}

      <input
        type="text"
        className="q-input apr-reason"
        disabled={submitting}
        placeholder={t('approvalReasonPh')}
        value={reason}
        onChange={(e) => setReason(e.target.value)}
      />

      {err && <div className="msg-err">{err}</div>}

      <div className="q-acts apr-acts">
        <button
          type="button"
          className="btn"
          disabled={submitting}
          onClick={() => void submit('reject', rejectOpt?.id, reason.trim() || undefined)}
        >
          {t('approvalReject')}
        </button>
        {allowAlwaysOpt && (
          <button
            type="button"
            className="btn"
            disabled={submitting}
            onClick={() => void submit('allow_always', allowAlwaysOpt.id)}
          >
            {t('approvalAllowAlways')}
          </button>
        )}
        <button
          type="button"
          className="btn go"
          disabled={submitting}
          onClick={() => void submit('allow_once', allowOnceOpt?.id)}
        >
          {t('approvalAllowOnce')}
        </button>
      </div>
    </div>
  );
};
