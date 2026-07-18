package com.elykia.apihub.data

import com.elykia.apihub.data.api.ApiClient
import com.elykia.apihub.data.model.AdapterDescriptor
import com.elykia.apihub.data.model.Announcement
import com.elykia.apihub.data.model.AnnouncementSync
import com.elykia.apihub.data.model.CheckinRun
import com.elykia.apihub.data.model.Site
import com.elykia.apihub.data.model.SitePatch
import com.elykia.apihub.data.model.SiteWrite
import com.elykia.apihub.data.model.Summary
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope

data class DashboardData(
    val summary: Summary,
    val adapters: List<AdapterDescriptor>,
    val sites: List<Site>,
    val checkins: List<CheckinRun>,
    val announcements: List<Announcement>,
)

interface ApiHubDataSource : AutoCloseable {
    suspend fun restore(): Boolean
    suspend fun connect(baseUrl: String, token: String): DashboardData
    suspend fun disconnect()
    suspend fun refresh(): DashboardData
    suspend fun createSite(input: SiteWrite): Site
    suspend fun patchSite(id: String, input: SitePatch): Site
    suspend fun runCheckin(siteId: String): CheckinRun
    suspend fun syncAnnouncements(siteId: String): AnnouncementSync
    suspend fun setAnnouncementRead(id: String, read: Boolean): Announcement
}

class ApiHubRepository(
    private val credentials: CredentialStore,
    private val clientFactory: (String, String) -> ApiClient = { baseUrl, token -> ApiClient(baseUrl, token) },
) : ApiHubDataSource {
    private var client: ApiClient? = null

    override suspend fun restore(): Boolean {
        val stored = credentials.load() ?: return false
        val candidate = clientFactory(stored.baseUrl, stored.token)
        return try {
            candidate.summary()
            replaceClient(candidate)
            true
        } catch (error: Exception) {
            candidate.close()
            throw error
        }
    }

    override suspend fun connect(baseUrl: String, token: String): DashboardData {
        require(token.isNotBlank()) { "管理员令牌不能为空" }
        val candidate = clientFactory(baseUrl, token)
        try {
            val dashboard = loadDashboard(candidate)
            credentials.save(StoredCredentials(candidate.baseUrl, token))
            replaceClient(candidate)
            return dashboard
        } catch (error: Exception) {
            candidate.close()
            throw error
        }
    }

    override suspend fun disconnect() {
        replaceClient(null)
        credentials.clear()
    }

    override suspend fun refresh(): DashboardData = loadDashboard(requireClient())

    private suspend fun loadDashboard(api: ApiClient): DashboardData = coroutineScope {
        val summary = async { api.summary() }
        val adapters = async { api.adapters() }
        val sites = async { api.sites() }
        val checkins = async { api.checkins() }
        val announcements = async { api.announcements() }
        DashboardData(
            summary = summary.await(),
            adapters = adapters.await(),
            sites = sites.await(),
            checkins = checkins.await(),
            announcements = announcements.await(),
        )
    }

    override suspend fun createSite(input: SiteWrite): Site = requireClient().createSite(input)
    override suspend fun patchSite(id: String, input: SitePatch): Site = requireClient().patchSite(id, input)
    override suspend fun runCheckin(siteId: String): CheckinRun = requireClient().runCheckin(siteId)
    override suspend fun syncAnnouncements(siteId: String): AnnouncementSync = requireClient().syncAnnouncements(siteId)
    override suspend fun setAnnouncementRead(id: String, read: Boolean): Announcement =
        requireClient().setAnnouncementRead(id, read)

    private fun requireClient(): ApiClient = checkNotNull(client) { "尚未连接服务器" }

    private fun replaceClient(next: ApiClient?) {
        client?.close()
        client = next
    }

    override fun close() {
        replaceClient(null)
    }
}
