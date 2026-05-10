import { useI18n } from 'vue-i18n'
import {
  AlertCircle,
  CheckCircle2,
  Clock3,
  GitMerge,
  LoaderCircle,
  ScanSearch,
  ShieldCheck,
  Sparkles,
  Workflow,
  Wrench,
  type LucideIcon,
} from 'lucide-vue-next'

export interface StatusMeta {
  label: string
  icon: LucideIcon
  dot: string
  chip: string
  task: string
}

export interface FlowKindMeta {
  icon: LucideIcon
  label: string
  color: string
}

export function useOrchestrationMeta() {
  const { t } = useI18n()

  function statusMeta(status: string): StatusMeta {
    switch (status) {
      case 'created':
        return {
          label: t('orchestration.statusPending'),
          icon: Clock3,
          dot: 'bg-muted-foreground',
          chip: 'border-border bg-muted/70 text-muted-foreground',
          task: 'border-border bg-muted/30',
        }
      case 'idle':
        return {
          label: t('orchestration.statusIdle'),
          icon: Clock3,
          dot: 'bg-muted-foreground',
          chip: 'border-border bg-muted/70 text-muted-foreground',
          task: 'border-border bg-muted/30',
        }
      case 'active':
        return {
          label: t('orchestration.statusActive'),
          icon: LoaderCircle,
          dot: 'bg-sky-500',
          chip: 'border-sky-500/20 bg-sky-500/10 text-sky-700 dark:text-sky-300',
          task: 'border-sky-500/30 bg-sky-500/8',
        }
      case 'completed':
        return {
          label: t('orchestration.statusSuccess'),
          icon: CheckCircle2,
          dot: 'bg-emerald-500',
          chip: 'border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
          task: 'bg-background',
        }
      case 'running':
        return {
          label: t('orchestration.statusRunning'),
          icon: LoaderCircle,
          dot: 'bg-sky-500',
          chip: 'border-sky-500/20 bg-sky-500/10 text-sky-700 dark:text-sky-300',
          task: 'border-sky-500/30 bg-sky-500/8',
        }
      case 'dispatching':
        return {
          label: t('orchestration.statusDispatching'),
          icon: LoaderCircle,
          dot: 'bg-sky-500',
          chip: 'border-sky-500/20 bg-sky-500/10 text-sky-700 dark:text-sky-300',
          task: 'border-sky-500/30 bg-sky-500/8',
        }
      case 'verifying':
        return {
          label: t('orchestration.statusVerifying'),
          icon: LoaderCircle,
          dot: 'bg-sky-500',
          chip: 'border-sky-500/20 bg-sky-500/10 text-sky-700 dark:text-sky-300',
          task: 'border-sky-500/30 bg-sky-500/8',
        }
      case 'waiting_human':
        return {
          label: t('orchestration.statusWaitingHuman'),
          icon: Clock3,
          dot: 'bg-amber-500',
          chip: 'border-amber-500/20 bg-amber-500/10 text-amber-700 dark:text-amber-300',
          task: 'border-amber-500/30 bg-amber-500/8',
        }
      case 'failed':
        return {
          label: t('orchestration.statusFailed'),
          icon: AlertCircle,
          dot: 'bg-rose-500',
          chip: 'border-rose-500/20 bg-rose-500/10 text-rose-700 dark:text-rose-300',
          task: 'border-rose-500/30 bg-rose-500/8',
        }
      case 'blocked':
        return {
          label: t('orchestration.statusBlocked'),
          icon: AlertCircle,
          dot: 'bg-rose-500',
          chip: 'border-rose-500/20 bg-rose-500/10 text-rose-700 dark:text-rose-300',
          task: 'border-rose-500/30 bg-rose-500/8',
        }
      case 'cancelled':
        return {
          label: t('orchestration.statusCancelled'),
          icon: AlertCircle,
          dot: 'bg-rose-500',
          chip: 'border-rose-500/20 bg-rose-500/10 text-rose-700 dark:text-rose-300',
          task: 'border-rose-500/30 bg-rose-500/8',
        }
      default:
        return {
          label: status ? status.replaceAll('_', ' ') : t('orchestration.statusPending'),
          icon: Clock3,
          dot: 'bg-muted-foreground',
          chip: 'border-border bg-muted/70 text-muted-foreground',
          task: 'border-border bg-muted/30',
        }
    }
  }

  function flowKindMeta(kind?: string): FlowKindMeta {
    switch (kind) {
      case 'planning':
        return {
          icon: Sparkles,
          label: t('orchestration.flowPlanning'),
          color: 'border-violet-500/25 bg-violet-500/10 text-violet-700 dark:text-violet-300',
        }
      case 'replanning':
        return {
          icon: GitMerge,
          label: t('orchestration.flowReplanning'),
          color: 'border-amber-500/25 bg-amber-500/10 text-amber-700 dark:text-amber-300',
        }
      case 'verification':
        return {
          icon: ShieldCheck,
          label: t('orchestration.flowVerification'),
          color: 'border-emerald-500/25 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
        }
      case 'checkpoint':
        return {
          icon: ScanSearch,
          label: t('orchestration.flowCheckpoint'),
          color: 'border-orange-500/25 bg-orange-500/10 text-orange-700 dark:text-orange-300',
        }
      case 'checkpoint_resume':
        return {
          icon: ScanSearch,
          label: t('orchestration.flowCheckpointResume'),
          color: 'border-orange-500/25 bg-orange-500/10 text-orange-700 dark:text-orange-300',
        }
      case 'attempt':
        return {
          icon: Wrench,
          label: t('orchestration.flowAttempt'),
          color: 'border-sky-500/25 bg-sky-500/10 text-sky-700 dark:text-sky-300',
        }
      case 'attempt_finalize':
        return {
          icon: Wrench,
          label: t('orchestration.flowAttemptFinalize'),
          color: 'border-sky-500/25 bg-sky-500/10 text-sky-700 dark:text-sky-300',
        }
      default:
        return {
          icon: Workflow,
          label: t('orchestration.flowStep'),
          color: 'border-border bg-muted/70 text-muted-foreground',
        }
    }
  }

  return { statusMeta, flowKindMeta }
}
