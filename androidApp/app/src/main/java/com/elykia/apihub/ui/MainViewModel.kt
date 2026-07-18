package com.elykia.apihub.ui

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.createSavedStateHandle
import androidx.lifecycle.viewmodel.CreationExtras
import androidx.lifecycle.viewModelScope
import com.elykia.apihub.data.ApiHubDataSource
import com.elykia.apihub.data.DashboardData
import com.elykia.apihub.data.api.ApiException
import com.elykia.apihub.data.model.AdapterDescriptor
import com.elykia.apihub.data.model.Announcement
import com.elykia.apihub.data.model.CheckinRun
import com.elykia.apihub.data.model.Site
import com.elykia.apihub.data.model.SitePatch
import com.elykia.apihub.data.model.SiteWrite
import com.elykia.apihub.data.model.Summary
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

enum class Screen { Overview, Sites, SiteEditor, Checkins, Announcements }

data class SiteDraft(
    val id: String? = null,
    val name: String = "",
    val baseUrl: String = "",
    val adapter: String = "auto",
    val userId: String = "",
    val accessToken: String = "",
    val enabled: Boolean = true,
    val checkinEnabled: Boolean = true,
    val announcementEnabled: Boolean = true,
    val checkinCron: String = "15 8 * * *",
    val announcementCron: String = "*/30 * * * *",
    val timezone: String = "Asia/Shanghai",
) {
    companion object {
        fun from(site: Site) = SiteDraft(
            id = site.id,
            name = site.name,
            baseUrl = site.baseUrl,
            adapter = site.adapter,
            userId = site.userId,
            enabled = site.enabled,
            checkinEnabled = site.checkinEnabled,
            announcementEnabled = site.announcementEnabled,
            checkinCron = site.checkinCron,
            announcementCron = site.announcementCron,
            timezone = site.timezone,
        )
    }
}

data class MainUiState(
    val restoring: Boolean = true,
    val connected: Boolean = false,
    val loading: Boolean = false,
    val screen: Screen = Screen.Overview,
    val summary: Summary? = null,
    val adapters: List<AdapterDescriptor> = emptyList(),
    val sites: List<Site> = emptyList(),
    val checkins: List<CheckinRun> = emptyList(),
    val announcements: List<Announcement> = emptyList(),
    val draft: SiteDraft = SiteDraft(),
    val inFlight: Set<String> = emptySet(),
    val error: String? = null,
)

class MainViewModel(
    private val repository: ApiHubDataSource,
    private val savedStateHandle: SavedStateHandle,
) : ViewModel() {
    private val mutableState = MutableStateFlow(
        MainUiState(
            screen = restoredScreen(),
            draft = restoredDraft(),
        ),
    )
    val state: StateFlow<MainUiState> = mutableState.asStateFlow()
    private var dashboardRequestVersion = 0L
    private var sessionVersion = 0L

    init {
        viewModelScope.launch {
            val restoreSessionVersion = sessionVersion
            val requestVersion = beginDashboardRequest()
            try {
                val restored = repository.restore()
                if (restored) {
                    applyDashboard(repository.refresh(), restoreSessionVersion, requestVersion)
                } else if (isCurrentSession(restoreSessionVersion) && isCurrentDashboardRequest(requestVersion)) {
                    clearPersistedUiState()
                    mutableState.update { it.copy(restoring = false, screen = Screen.Overview, draft = SiteDraft()) }
                }
            } catch (error: CancellationException) {
                throw error
            } catch (error: Exception) {
                if (!isCurrentSession(restoreSessionVersion) || !isCurrentDashboardRequest(requestVersion)) return@launch
                if (error is ApiException && error.status == 401) {
                    invalidateSession()
                    clearPersistedUiState()
                }
                val cleanupFailed = error is ApiException && error.status == 401 && !clearUnauthorizedSession()
                mutableState.update {
                    it.copy(
                        restoring = false,
                        connected = false,
                        screen = if (error is ApiException && error.status == 401) Screen.Overview else it.screen,
                        draft = if (error is ApiException && error.status == 401) SiteDraft() else it.draft,
                        error = if (cleanupFailed) "管理员令牌已失效，且本地凭据清理失败" else messageFor(error),
                    )
                }
            }
        }
    }

    fun connect(baseUrl: String, token: String) = launchAction(CONNECT_ACTION) { actionSessionVersion ->
        loadDashboard(actionSessionVersion) { repository.connect(baseUrl, token) }
    }

    fun disconnect() {
        invalidateSession()
        clearPersistedUiState()
        launchAction(DISCONNECT_ACTION) {
            try {
                repository.disconnect()
            } finally {
                mutableState.value = MainUiState(restoring = false)
            }
        }
    }

    fun navigate(screen: Screen) {
        mutableState.update { it.copy(screen = screen, error = null) }
        persistScreen(screen)
    }

    fun refresh() = launchAction(REFRESH_ACTION) { actionSessionVersion ->
        loadDashboard(actionSessionVersion, repository::refresh)
    }

    fun newSite() {
        val draft = SiteDraft()
        mutableState.update { it.copy(screen = Screen.SiteEditor, draft = draft, error = null) }
        persistScreen(Screen.SiteEditor)
        persistDraft(draft)
    }

    fun editSite(site: Site) {
        val draft = SiteDraft.from(site)
        mutableState.update { it.copy(screen = Screen.SiteEditor, draft = draft, error = null) }
        persistScreen(Screen.SiteEditor)
        persistDraft(draft)
    }

    fun updateDraft(transform: (SiteDraft) -> SiteDraft) {
        val draft = transform(mutableState.value.draft)
        mutableState.update { it.copy(draft = draft, error = null) }
        persistDraft(draft)
    }

    fun selectAdapter(adapter: String) {
        mutableState.update { state ->
            val descriptor = state.adapters.firstOrNull { it.name == adapter }
            if (adapter != "auto" && descriptor == null) {
                return@update state.copy(error = "请选择有效的站点类型")
            }
            val draft = state.draft
            state.copy(
                draft = draft.copy(
                    adapter = adapter,
                    userId = if (draft.id == null && descriptor?.capabilities?.requiresUserId == false) "" else draft.userId,
                    checkinEnabled = when {
                        adapter == "auto" -> draft.checkinEnabled
                        descriptor?.capabilities?.checkin == false -> false
                        draft.id == null -> true
                        else -> draft.checkinEnabled
                    },
                    announcementEnabled = when {
                        adapter == "auto" -> draft.announcementEnabled
                        descriptor?.capabilities?.announcements == false -> false
                        draft.id == null -> true
                        else -> draft.announcementEnabled
                    },
                ),
                error = null,
            )
        }
        persistDraft(mutableState.value.draft)
    }

    fun saveSite() = launchAction(SAVE_SITE_ACTION) { actionSessionVersion ->
        val snapshot = mutableState.value
        val draft = snapshot.draft.validated(snapshot.adapters)
        if (draft.id == null) {
            require(draft.accessToken.isNotEmpty()) { "新站点必须填写访问令牌" }
            repository.createSite(
                SiteWrite(
                    name = draft.name,
                    baseUrl = draft.baseUrl,
                    adapter = draft.adapter,
                    userId = draft.userId,
                    accessToken = draft.accessToken,
                    enabled = draft.enabled,
                    checkinEnabled = draft.checkinEnabled.takeUnless { draft.adapter == "auto" },
                    announcementEnabled = draft.announcementEnabled.takeUnless { draft.adapter == "auto" },
                    checkinCron = draft.checkinCron,
                    announcementCron = draft.announcementCron,
                    timezone = draft.timezone,
                ),
            )
        } else {
            repository.patchSite(
                draft.id,
                SitePatch(
                    name = draft.name,
                    baseUrl = draft.baseUrl,
                    adapter = draft.adapter,
                    userId = draft.userId,
                    accessToken = draft.accessToken.takeIf { it.isNotEmpty() },
                    enabled = draft.enabled,
                    checkinEnabled = draft.checkinEnabled,
                    announcementEnabled = draft.announcementEnabled,
                    checkinCron = draft.checkinCron,
                    announcementCron = draft.announcementCron,
                    timezone = draft.timezone,
                ),
            )
        }
        if (loadDashboard(actionSessionVersion, repository::refresh)) {
            mutableState.update { it.copy(screen = Screen.Sites) }
            persistScreen(Screen.Sites)
        }
    }

    fun toggleSite(site: Site) = launchAction("toggle:${site.id}") { actionSessionVersion ->
        repository.patchSite(site.id, SitePatch(enabled = !site.enabled))
        loadDashboard(actionSessionVersion, repository::refresh)
    }

    fun runCheckin(site: Site) = launchAction("checkin:${site.id}") { actionSessionVersion ->
        repository.runCheckin(site.id)
        loadDashboard(actionSessionVersion, repository::refresh)
    }

    fun syncAnnouncements(site: Site) = launchAction("sync:${site.id}") { actionSessionVersion ->
        repository.syncAnnouncements(site.id)
        loadDashboard(actionSessionVersion, repository::refresh)
    }

    fun setAnnouncementRead(announcement: Announcement, read: Boolean) =
        launchAction("announcement:${announcement.id}") { actionSessionVersion ->
            repository.setAnnouncementRead(announcement.id, read)
            loadDashboard(actionSessionVersion, repository::refresh)
        }

    private fun launchAction(key: String, block: suspend (Long) -> Unit) {
        if (key in mutableState.value.inFlight) return
        val actionSessionVersion = sessionVersion
        viewModelScope.launch {
            if (!isCurrentSession(actionSessionVersion)) return@launch
            mutableState.update { it.copy(inFlight = it.inFlight + key, loading = key == CONNECT_ACTION, error = null) }
            try {
                block(actionSessionVersion)
            } catch (error: CancellationException) {
                throw error
            } catch (error: Exception) {
                if (!isCurrentSession(actionSessionVersion)) return@launch
                var cleanupFailed = false
                if (error is ApiException && error.status == 401) {
                    invalidateSession()
                    clearPersistedUiState()
                    cleanupFailed = !clearUnauthorizedSession()
                    mutableState.update { it.copy(connected = false, screen = Screen.Overview, draft = SiteDraft()) }
                }
                mutableState.update {
                    it.copy(error = if (cleanupFailed) "管理员令牌已失效，且本地凭据清理失败" else messageFor(error))
                }
            } finally {
                mutableState.update {
                    val remaining = it.inFlight - key
                    it.copy(
                        inFlight = remaining,
                        loading = CONNECT_ACTION in remaining,
                        restoring = if (isCurrentSession(actionSessionVersion)) false else it.restoring,
                    )
                }
            }
        }
    }

    private suspend fun loadDashboard(
        actionSessionVersion: Long,
        load: suspend () -> DashboardData,
    ): Boolean {
        if (!isCurrentSession(actionSessionVersion)) return false
        val requestVersion = beginDashboardRequest()
        return try {
            applyDashboard(load(), actionSessionVersion, requestVersion)
        } catch (error: CancellationException) {
            throw error
        } catch (error: Exception) {
            if (isCurrentSession(actionSessionVersion) && isCurrentDashboardRequest(requestVersion)) throw error
            false
        }
    }

    private fun applyDashboard(
        data: DashboardData,
        actionSessionVersion: Long,
        requestVersion: Long,
    ): Boolean {
        if (!isCurrentSession(actionSessionVersion) || !isCurrentDashboardRequest(requestVersion)) return false
        mutableState.update {
            it.copy(
                restoring = false,
                connected = true,
                summary = data.summary,
                adapters = data.adapters,
                sites = data.sites,
                checkins = data.checkins,
                announcements = data.announcements,
                error = null,
            )
        }
        return true
    }

    private fun beginDashboardRequest(): Long = ++dashboardRequestVersion

    private fun invalidateSession() {
        sessionVersion++
        dashboardRequestVersion++
    }

    private fun isCurrentSession(actionSessionVersion: Long): Boolean =
        actionSessionVersion == sessionVersion

    private fun isCurrentDashboardRequest(requestVersion: Long): Boolean =
        requestVersion == dashboardRequestVersion

    private fun SiteDraft.validated(adapters: List<AdapterDescriptor>): SiteDraft {
        require(name.isNotBlank()) { "站点名称不能为空" }
        require(baseUrl.isNotBlank()) { "站点地址不能为空" }
        val descriptor = adapters.firstOrNull { it.name == adapter }
        require(adapter == "auto" || descriptor != null) { "请选择有效的站点类型" }
        require(descriptor?.capabilities?.requiresUserId != true || userId.isNotBlank()) { "此站点类型必须填写用户 ID" }
        require(checkinCron.isNotBlank() && announcementCron.isNotBlank()) { "Cron 表达式不能为空" }
        require(timezone.isNotBlank()) { "时区不能为空" }
        return copy(
            name = name.trim(),
            baseUrl = baseUrl.trim().trimEnd('/'),
            userId = userId.trim(),
            checkinCron = checkinCron.trim(),
            announcementCron = announcementCron.trim(),
            timezone = timezone.trim(),
        )
    }

    private fun messageFor(error: Exception): String = when (error) {
        is ApiException -> error.message
        is IllegalArgumentException -> error.message ?: "输入无效"
        else -> "操作失败，请稍后重试"
    }

    private suspend fun clearUnauthorizedSession(): Boolean = try {
        repository.disconnect()
        true
    } catch (error: CancellationException) {
        throw error
    } catch (_: Exception) {
        false
    }

    private fun restoredScreen(): Screen = savedStateHandle.get<String>(SCREEN_KEY)
        ?.let { stored -> Screen.entries.firstOrNull { it.name == stored } }
        ?: Screen.Overview

    private fun restoredDraft(): SiteDraft {
        val default = SiteDraft()
        return default.copy(
            id = savedStateHandle[DRAFT_ID_KEY],
            name = savedStateHandle[DRAFT_NAME_KEY] ?: default.name,
            baseUrl = savedStateHandle[DRAFT_BASE_URL_KEY] ?: default.baseUrl,
            adapter = savedStateHandle[DRAFT_ADAPTER_KEY] ?: default.adapter,
            userId = savedStateHandle[DRAFT_USER_ID_KEY] ?: default.userId,
            enabled = savedStateHandle[DRAFT_ENABLED_KEY] ?: default.enabled,
            checkinEnabled = savedStateHandle[DRAFT_CHECKIN_ENABLED_KEY] ?: default.checkinEnabled,
            announcementEnabled = savedStateHandle[DRAFT_ANNOUNCEMENT_ENABLED_KEY] ?: default.announcementEnabled,
            checkinCron = savedStateHandle[DRAFT_CHECKIN_CRON_KEY] ?: default.checkinCron,
            announcementCron = savedStateHandle[DRAFT_ANNOUNCEMENT_CRON_KEY] ?: default.announcementCron,
            timezone = savedStateHandle[DRAFT_TIMEZONE_KEY] ?: default.timezone,
            accessToken = "",
        )
    }

    private fun persistScreen(screen: Screen) {
        savedStateHandle[SCREEN_KEY] = screen.name
    }

    private fun persistDraft(draft: SiteDraft) {
        savedStateHandle[DRAFT_ID_KEY] = draft.id
        savedStateHandle[DRAFT_NAME_KEY] = draft.name
        savedStateHandle[DRAFT_BASE_URL_KEY] = draft.baseUrl
        savedStateHandle[DRAFT_ADAPTER_KEY] = draft.adapter
        savedStateHandle[DRAFT_USER_ID_KEY] = draft.userId
        savedStateHandle[DRAFT_ENABLED_KEY] = draft.enabled
        savedStateHandle[DRAFT_CHECKIN_ENABLED_KEY] = draft.checkinEnabled
        savedStateHandle[DRAFT_ANNOUNCEMENT_ENABLED_KEY] = draft.announcementEnabled
        savedStateHandle[DRAFT_CHECKIN_CRON_KEY] = draft.checkinCron
        savedStateHandle[DRAFT_ANNOUNCEMENT_CRON_KEY] = draft.announcementCron
        savedStateHandle[DRAFT_TIMEZONE_KEY] = draft.timezone
    }

    private fun clearPersistedUiState() {
        PERSISTED_UI_KEYS.forEach { savedStateHandle.remove<Any>(it) }
    }

    override fun onCleared() {
        repository.close()
        super.onCleared()
    }

    class Factory(
        private val repository: ApiHubDataSource,
    ) : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T {
            require(modelClass.isAssignableFrom(MainViewModel::class.java))
            throw UnsupportedOperationException("CreationExtras with SavedStateHandle are required")
        }

        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>, extras: CreationExtras): T {
            require(modelClass.isAssignableFrom(MainViewModel::class.java))
            return MainViewModel(repository, extras.createSavedStateHandle()) as T
        }
    }

    companion object {
        private const val CONNECT_ACTION = "connect"
        private const val DISCONNECT_ACTION = "disconnect"
        private const val REFRESH_ACTION = "refresh"
        private const val SAVE_SITE_ACTION = "save-site"
        private const val SCREEN_KEY = "screen"
        private const val DRAFT_ID_KEY = "draft.id"
        private const val DRAFT_NAME_KEY = "draft.name"
        private const val DRAFT_BASE_URL_KEY = "draft.base_url"
        private const val DRAFT_ADAPTER_KEY = "draft.adapter"
        private const val DRAFT_USER_ID_KEY = "draft.user_id"
        private const val DRAFT_ENABLED_KEY = "draft.enabled"
        private const val DRAFT_CHECKIN_ENABLED_KEY = "draft.checkin_enabled"
        private const val DRAFT_ANNOUNCEMENT_ENABLED_KEY = "draft.announcement_enabled"
        private const val DRAFT_CHECKIN_CRON_KEY = "draft.checkin_cron"
        private const val DRAFT_ANNOUNCEMENT_CRON_KEY = "draft.announcement_cron"
        private const val DRAFT_TIMEZONE_KEY = "draft.timezone"
        private val PERSISTED_UI_KEYS = setOf(
            SCREEN_KEY,
            DRAFT_ID_KEY,
            DRAFT_NAME_KEY,
            DRAFT_BASE_URL_KEY,
            DRAFT_ADAPTER_KEY,
            DRAFT_USER_ID_KEY,
            DRAFT_ENABLED_KEY,
            DRAFT_CHECKIN_ENABLED_KEY,
            DRAFT_ANNOUNCEMENT_ENABLED_KEY,
            DRAFT_CHECKIN_CRON_KEY,
            DRAFT_ANNOUNCEMENT_CRON_KEY,
            DRAFT_TIMEZONE_KEY,
        )
    }
}
