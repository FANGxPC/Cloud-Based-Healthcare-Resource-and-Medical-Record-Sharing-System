import { type TransferStatus } from '../api';
import { Badge } from './common';

const TONE: Record<TransferStatus, 'good' | 'bad' | 'warn' | 'neutral'> = {
  QUEUED: 'neutral',
  PENDING_APPROVAL: 'warn',
  ACCEPTED: 'good',
  REJECTED: 'bad',
  NO_CAPACITY: 'bad',
  FAILED: 'bad',
};

export default function TransferStatusBadge({ status }: { status: TransferStatus }) {
  return <Badge tone={TONE[status]}>{status.replace('_', ' ')}</Badge>;
}
