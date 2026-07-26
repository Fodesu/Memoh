import type { SessionruntimeSnapshot } from '@memohai/sdk'
import type {
  SessionRuntimeDeltaEvent,
  SessionRuntimeSnapshotEvent,
  SessionRuntimeStateEvent,
} from '@memohai/sdk/session-runtime'
import generationReuseJSON from './__fixtures__/runtime-generation-reuse.contract.json'
import interruptedRunJSON from './__fixtures__/runtime-interrupted-run.contract.json'
import recoveryJSON from './__fixtures__/runtime-recovery.contract.json'
import replacementOperationsJSON from './__fixtures__/runtime-replacement-operations.contract.json'
import richActiveRunJSON from './__fixtures__/runtime-rich-active-run.contract.json'

export interface RuntimeContractFixture {
  version: number
  scenario: 'rich_active_run' | 'interrupted_run'
  runtime_snapshot: SessionRuntimeSnapshotEvent & {
    bot_id: string
    session_id: string
    seq: number
    snapshot: SessionruntimeSnapshot
  }
  runtime_stream: SessionRuntimeStateEvent[]
  runtime_terminal_stream?: SessionRuntimeStateEvent[]
  runtime_abort_stream?: SessionRuntimeStateEvent[]
  runtime_admission_stream?: SessionRuntimeStateEvent[]
  runtime_reset_stream?: SessionRuntimeStateEvent[]
  runtime_steer_stream?: SessionRuntimeStateEvent[]
}

export interface RuntimeReplacementContractFixture {
  version: number
  retry_snapshot: RuntimeContractFixture['runtime_snapshot']
  edit_snapshot: RuntimeContractFixture['runtime_snapshot']
}

export interface RuntimeGenerationReuseContractFixture {
  version: number
  scenario: 'generation_reuse'
  runtime_snapshot: RuntimeContractFixture['runtime_snapshot']
  runtime_stream: SessionRuntimeStateEvent[]
}

export interface RuntimeRecoveryContractFixture {
  version: number
  scenario: 'gap_checkpoint_recovery'
  runtime_snapshot: RuntimeContractFixture['runtime_snapshot']
  gap_delta: SessionRuntimeDeltaEvent
  delayed_delta: SessionRuntimeDeltaEvent
  runtime_checkpoint: RuntimeContractFixture['runtime_snapshot']
  post_recovery_delta: SessionRuntimeDeltaEvent
}

export const richActiveRunContractFixture = richActiveRunJSON as unknown as RuntimeContractFixture
export const interruptedRunContractFixture = interruptedRunJSON as unknown as RuntimeContractFixture
export const replacementOperationsContractFixture = replacementOperationsJSON as unknown as RuntimeReplacementContractFixture
export const generationReuseContractFixture = generationReuseJSON as unknown as RuntimeGenerationReuseContractFixture
export const runtimeRecoveryContractFixture = recoveryJSON as unknown as RuntimeRecoveryContractFixture
