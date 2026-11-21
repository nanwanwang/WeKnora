import { del, get, getDown } from '@/utils/request'

/**
 * One file in the artifact library: the latest version of a file a skill
 * generated in one of the caller's sessions. Download it through
 * downloadArtifact(session_id, message_id, index) from '@/api/chat', which
 * re-checks session ownership server side.
 */
export interface ArtifactLibraryItem {
  session_id: string
  session_title: string
  message_id: string
  index: number
  /** `resource://<handle>` when the deployment runs a resource catalog. */
  handle?: string
  file_name: string
  file_type: string
  file_size: number
  source_path: string
  created_at: string
  /** How many times this file was (re)generated in its session. */
  version_count: number
  mom?: { fileId: string; canPreview: boolean; canDownload: boolean }
}

export interface ArtifactLibraryParams {
  keyword?: string
  /** Extensions such as ".pdf"; empty means every type. */
  fileTypes?: string[]
  page?: number
  pageSize?: number
}

export interface ArtifactLibraryPage {
  success: boolean
  data: ArtifactLibraryItem[]
  total: number
  page: number
  page_size: number
}

export function listArtifactLibrary(params: ArtifactLibraryParams = {}) {
  return get<ArtifactLibraryPage>('/api/v1/artifacts', { params: artifactQuery(params) })
}

function artifactQuery(params: ArtifactLibraryParams) {
  const query: Record<string, string | number> = {}
  const keyword = params.keyword?.trim()
  if (keyword) query.keyword = keyword
  if (params.fileTypes?.length) query.file_types = params.fileTypes.join(',')
  if (params.page) query.page = params.page
  if (params.pageSize) query.page_size = params.pageSize
  return query
}

interface MomArtifact {
  messageId: string
  fileId: string
  fileName: string
  fileType: string
  fileSize?: number
  createdAt: string
  canPreview: boolean
  canDownload: boolean
}

export async function listMomArtifacts(params: ArtifactLibraryParams) {
  const page = await get<{ data: MomArtifact[]; has_more: boolean }>('/api/v1/mom-artifacts', { params: artifactQuery(params) })
  const data: ArtifactLibraryItem[] = page.data.map(item => ({
    session_id: '', session_title: '', message_id: item.messageId, index: 0, source_path: '', version_count: 1,
    file_name: item.fileName, file_type: item.fileType, file_size: item.fileSize ?? 0, created_at: item.createdAt,
    mom: { fileId: item.fileId, canPreview: item.canPreview, canDownload: item.canDownload },
  }))
  return { data, hasMore: page.has_more }
}

export function readMomArtifact(item: ArtifactLibraryItem, action: 'preview' | 'download') {
  if (!item.mom) throw new Error('Missing MOM file identity')
  return getDown(`/api/v1/mom-artifacts/${encodeURIComponent(item.message_id)}/${encodeURIComponent(item.mom.fileId)}/${action}`)
}

export interface ArtifactDeleteResult {
  success: boolean
  data: { file_name: string; deleted: number }
}

/**
 * Deletes a file from the artifact library, along with every earlier version of
 * it in the same session — a library row is the file, not one regeneration of
 * it, so leaving the older versions behind would leave the row in place.
 *
 * The stored bytes are reclaimed unless something else still references them,
 * so this is not reversible. Callers confirm with the user first.
 */
export function deleteArtifactLibraryItem(item: ArtifactLibraryItem) {
  const query = new URLSearchParams({
    session_id: item.session_id,
    message_id: item.message_id,
    index: String(item.index),
  })
  return del<ArtifactDeleteResult>(`/api/v1/artifacts?${query.toString()}`)
}
