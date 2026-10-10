import { type TransferStatus } from '../api';
import { Badge } from './common';

const TONE: Record<TransferStatus, 'good' | 'bad' | 'warn' | 'neutral' | 'info'> = {
  QUEUED: 'info',
  PENDING_APPROVAL: 'warn',
  ACCEPTED: 'good',
  REJECTED: 'bad',
  NO_CAPACITY: 'bad',
  FAILED: 'bad',
};

const LABEL: Record<TransferStatus, string> = {
  QUEUED: 'Queued',
  PENDING_APPROVAL: 'Pending approval',
  ACCEPTED: 'Accepted',
  REJECTED: 'Rejected',
  NO_CAPACITY: 'No capacity',
  FAILED: 'Failed',
};

export default function TransferStatusBadge({ status }: { status: TransferStatus }) {
  return <Badge tone={TONE[status]}>{LABEL[status]}</Badge>;
}
