package com.elykia.apihub.ui

import androidx.lifecycle.SavedStateHandle
import com.elykia.apihub.data.ApiHubDataSource
import com.elykia.apihub.data.DashboardData
import com.elykia.apihub.data.api.ApiException
import com.elykia.apihub.data.model.AdapterDescriptor
import com.elykia.apihub.data.model.Announcement
import com.elykia.apihub.data.model.AnnouncementSync
import com.elykia.apihub.data.model.Capabilities
import com.elykia.apihub.data.model.CheckinRun
import com.elykia.apihub.data.model.Site
import com.elykia.apihub.data.model.SiteCounts
import com.elykia.apihub.data.model.SitePatch
import com.elykia.apihub.data.model.SiteWrite
import com.elykia.apihub.data.model.Summary
import com.google.common.truth.Truth.assertThat
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Before
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class MainViewModelTest {
    private val dispatcher = StandardTestDispatcher()

    @Before
    fun setUp() {
        Dispatchers.setMain(dispatcher)
    }

    @After
    fun tearDown() {
        Dispatchers.resetMain()
    }

    @Test
    fun connectPublishesDashboard() = runTest(dispatcher) {
        val repository = FakeRepository()
        val viewModel = MainViewModel(repository, SavedStateHandle())
        advanceUntilIdle()

        viewModel.connect("https://hub.example", "token")
        advanceUntilIdle()

        assertThat(viewModel.state.value.connected).isTrue()
        assertThat(viewModel.state.value.adapters).containsExactlyElementsIn(repository.adapters)
        assertThat(viewModel.state.value.sites).containsExactly(repository.site)
        assertThat(viewModel.state.value.summary?.sites?.total).isEqualTo(1)
    }

    @Test
    fun processRecreationRestoresPersistedSession() = runTest(dispatcher) {
        val repository = FakeRepository(restoreResult = true)

        val viewModel = MainViewModel(repository, SavedStateHandle())
        advanceUntilIdle()

        assertThat(viewModel.state.value.restoring).isFalse()
        assertThat(viewModel.state.value.connected).isTrue()
        assertThat(viewModel.state.value.sites).containsExactly(repository.site)
    }

    @Test
    fun processRecreationRestoresOnlyNonSensitiveEditorState() = runTest(dispatcher) {
        val handle = SavedStateHandle()
        val viewModel = MainViewModel(FakeRepository(), handle)
        advanceUntilIdle()
        viewModel.connect("https://hub.example", "admin-token")
        advanceUntilIdle()
        viewModel.newSite()
        viewModel.updateDraft {
            it.copy(
                name = "恢复站点",
                baseUrl = "https://station.example",
                userId = "42",
                accessToken = "station-secret",
                timezone = "UTC",
            )
        }

        val snapshot = handle.keys().associateWith { key -> handle.get<Any?>(key) }
        assertThat(snapshot.values).doesNotContain("station-secret")
        assertThat(snapshot.values).doesNotContain("admin-token")

        val recreated = MainViewModel(
            FakeRepository(restoreResult = true),
            SavedStateHandle(snapshot),
        )
        advanceUntilIdle()

        assertThat(recreated.state.value.connected).isTrue()
        assertThat(recreated.state.value.screen).isEqualTo(Screen.SiteEditor)
        assertThat(recreated.state.value.draft.name).isEqualTo("恢复站点")
        assertThat(recreated.state.value.draft.baseUrl).isEqualTo("https://station.example")
        assertThat(recreated.state.value.draft.userId).isEqualTo("42")
        assertThat(recreated.state.value.draft.timezone).isEqualTo("UTC")
        assertThat(recreated.state.value.draft.accessToken).isEmpty()
    }

    @Test
    fun revokedPersistedSessionIsClearedDuringRestore() = runTest(dispatcher) {
        val repository = FakeRepository(
            restoreFailure = ApiException(401, "AUTH_REQUIRED", "令牌已失效", false, "request-1"),
        )

        val viewModel = MainViewModel(repository, SavedStateHandle())
        advanceUntilIdle()

        assertThat(repository.disconnectCalled).isTrue()
        assertThat(viewModel.state.value.connected).isFalse()
        assertThat(viewModel.state.value.error).isEqualTo("令牌已失效")
    }

    @Test
    fun failedCredentialCleanupStillDisconnectsUi() = runTest(dispatcher) {
        val repository = FakeRepository(disconnectFailure = IllegalStateException("storage unavailable"))
        val viewModel = MainViewModel(repository, SavedStateHandle())
        advanceUntilIdle()
        viewModel.connect("https://hub.example", "token")
        advanceUntilIdle()

        viewModel.disconnect()
        advanceUntilIdle()

        assertThat(repository.disconnectCalled).isTrue()
        assertThat(viewModel.state.value.connected).isFalse()
        assertThat(viewModel.state.value.error).isEqualTo("操作失败，请稍后重试")
    }

    @Test
    fun toggleSitePreservesExplicitFalsePatch() = runTest(dispatcher) {
        val repository = FakeRepository()
        val viewModel = MainViewModel(repository, SavedStateHandle())
        advanceUntilIdle()
        viewModel.connect("https://hub.example", "token")
        advanceUntilIdle()

        viewModel.toggleSite(repository.site)
        advanceUntilIdle()

        assertThat(repository.lastPatch?.enabled).isFalse()
    }

    @Test
    fun automaticAdapterCreateOmitsCapabilityOverrides() = runTest(dispatcher) {
        val repository = FakeRepository()
        val viewModel = MainViewModel(repository, SavedStateHandle())
        advanceUntilIdle()
        viewModel.newSite()
        viewModel.updateDraft {
            it.copy(name = "自动站点", baseUrl = "https://station.example", adapter = "auto", accessToken = "token")
        }

        viewModel.saveSite()
        advanceUntilIdle()

        assertThat(repository.lastCreate?.checkinEnabled).isNull()
        assertThat(repository.lastCreate?.announcementEnabled).isNull()
    }

    @Test
    fun siteCredentialsPreserveWhitespaceForCreateAndPatch() = runTest(dispatcher) {
        val repository = FakeRepository()
        val viewModel = MainViewModel(repository, SavedStateHandle())
        advanceUntilIdle()
        viewModel.connect("https://hub.example", "admin-token")
        advanceUntilIdle()
        viewModel.newSite()
        viewModel.updateDraft {
            it.copy(name = "站点", baseUrl = "https://station.example", accessToken = "  create-token\n")
        }

        viewModel.saveSite()
        advanceUntilIdle()

        assertThat(repository.lastCreate?.accessToken).isEqualTo("  create-token\n")

        viewModel.editSite(repository.site)
        viewModel.updateDraft { it.copy(userId = "42", accessToken = "\tpatch-token  ") }
        viewModel.saveSite()
        advanceUntilIdle()

        assertThat(repository.lastPatch?.accessToken).isEqualTo("\tpatch-token  ")
    }

    @Test
    fun selectingAdapterAppliesServerCapabilities() = runTest(dispatcher) {
        val repository = FakeRepository()
        val viewModel = MainViewModel(repository, SavedStateHandle())
        advanceUntilIdle()
        viewModel.connect("https://hub.example", "token")
        advanceUntilIdle()
        viewModel.newSite()

        viewModel.selectAdapter("sub2api")

        assertThat(viewModel.state.value.draft.checkinEnabled).isFalse()
        assertThat(viewModel.state.value.draft.announcementEnabled).isTrue()
        assertThat(viewModel.state.value.draft.userId).isEmpty()

        viewModel.selectAdapter("zen-api")

        assertThat(viewModel.state.value.draft.checkinEnabled).isTrue()
        assertThat(viewModel.state.value.draft.announcementEnabled).isFalse()
    }

    @Test
    fun automaticAdapterPatchPreservesCapabilityOverrides() = runTest(dispatcher) {
        val repository = FakeRepository()
        val viewModel = MainViewModel(repository, SavedStateHandle())
        advanceUntilIdle()
        viewModel.connect("https://hub.example", "token")
        advanceUntilIdle()
        viewModel.editSite(repository.site)
        viewModel.selectAdapter("auto")

        viewModel.saveSite()
        advanceUntilIdle()

        assertThat(repository.lastPatch?.adapter).isEqualTo("auto")
        assertThat(repository.lastPatch?.checkinEnabled).isTrue()
        assertThat(repository.lastPatch?.announcementEnabled).isTrue()
    }

    @Test
    fun olderDashboardSuccessCannotOverwriteNewerRefresh() = runTest(dispatcher) {
        val repository = FakeRepository()
        val olderRefresh = repository.enqueueRefresh()
        val newerRefresh = repository.enqueueRefresh()
        val viewModel = MainViewModel(repository, SavedStateHandle())
        advanceUntilIdle()
        viewModel.connect("https://hub.example", "token")
        advanceUntilIdle()

        viewModel.refresh()
        runCurrent()
        viewModel.toggleSite(repository.site)
        runCurrent()

        assertThat(viewModel.state.value.inFlight)
            .containsExactly("refresh", "toggle:${repository.site.id}")

        newerRefresh.complete(repository.dashboard(totalSites = 2))
        runCurrent()

        assertThat(viewModel.state.value.summary?.sites?.total).isEqualTo(2)
        assertThat(viewModel.state.value.inFlight).containsExactly("refresh")

        olderRefresh.complete(repository.dashboard(totalSites = 1))
        advanceUntilIdle()

        assertThat(viewModel.state.value.summary?.sites?.total).isEqualTo(2)
        assertThat(viewModel.state.value.inFlight).isEmpty()
        assertThat(viewModel.state.value.error).isNull()
    }

    @Test
    fun unauthorizedRefreshCannotBeReversedByOlderSuccess() = runTest(dispatcher) {
        val repository = FakeRepository()
        val olderRefresh = repository.enqueueRefresh()
        val unauthorizedRefresh = repository.enqueueRefresh()
        val viewModel = MainViewModel(repository, SavedStateHandle())
        advanceUntilIdle()
        viewModel.connect("https://hub.example", "token")
        advanceUntilIdle()

        viewModel.refresh()
        runCurrent()
        viewModel.toggleSite(repository.site)
        runCurrent()

        unauthorizedRefresh.completeExceptionally(
            ApiException(401, "AUTH_REQUIRED", "令牌已失效", false, "request-2"),
        )
        runCurrent()

        assertThat(repository.disconnectCalled).isTrue()
        assertThat(viewModel.state.value.connected).isFalse()
        assertThat(viewModel.state.value.error).isEqualTo("令牌已失效")
        assertThat(viewModel.state.value.inFlight).containsExactly("refresh")

        olderRefresh.complete(repository.dashboard(totalSites = 1))
        advanceUntilIdle()

        assertThat(viewModel.state.value.connected).isFalse()
        assertThat(viewModel.state.value.error).isEqualTo("令牌已失效")
        assertThat(viewModel.state.value.inFlight).isEmpty()
    }

    @Test
    fun staleSaveCannotNavigateAfterUnauthorizedRefresh() = runTest(dispatcher) {
        val repository = FakeRepository()
        val saveRefresh = repository.enqueueRefresh()
        val unauthorizedRefresh = repository.enqueueRefresh()
        val viewModel = MainViewModel(repository, SavedStateHandle())
        advanceUntilIdle()
        viewModel.connect("https://hub.example", "token")
        advanceUntilIdle()
        viewModel.editSite(repository.site)
        viewModel.updateDraft { it.copy(userId = "42") }

        viewModel.saveSite()
        runCurrent()
        viewModel.refresh()
        runCurrent()

        unauthorizedRefresh.completeExceptionally(
            ApiException(401, "AUTH_REQUIRED", "令牌已失效", false, "request-3"),
        )
        runCurrent()

        assertThat(viewModel.state.value.connected).isFalse()
        assertThat(viewModel.state.value.screen).isEqualTo(Screen.Overview)
        assertThat(viewModel.state.value.inFlight).containsExactly("save-site")

        saveRefresh.complete(repository.dashboard(totalSites = 1))
        advanceUntilIdle()

        assertThat(viewModel.state.value.connected).isFalse()
        assertThat(viewModel.state.value.screen).isEqualTo(Screen.Overview)
        assertThat(viewModel.state.value.error).isEqualTo("令牌已失效")
        assertThat(viewModel.state.value.inFlight).isEmpty()
    }

    @Test
    fun actionWhoseMutationFinishesAfterUnauthorizedRefreshCannotReloadDashboard() = runTest(dispatcher) {
        val patchGate = CompletableDeferred<Unit>()
        val repository = FakeRepository(patchGate = patchGate)
        val unauthorizedRefresh = repository.enqueueRefresh()
        val viewModel = MainViewModel(repository, SavedStateHandle())
        advanceUntilIdle()
        viewModel.connect("https://hub.example", "token")
        advanceUntilIdle()

        viewModel.toggleSite(repository.site)
        runCurrent()
        viewModel.refresh()
        runCurrent()

        unauthorizedRefresh.completeExceptionally(
            ApiException(401, "AUTH_REQUIRED", "令牌已失效", false, "request-4"),
        )
        runCurrent()

        assertThat(viewModel.state.value.connected).isFalse()
        assertThat(viewModel.state.value.error).isEqualTo("令牌已失效")
        assertThat(repository.refreshCallCount).isEqualTo(1)

        patchGate.complete(Unit)
        advanceUntilIdle()

        assertThat(repository.refreshCallCount).isEqualTo(1)
        assertThat(viewModel.state.value.connected).isFalse()
        assertThat(viewModel.state.value.error).isEqualTo("令牌已失效")
        assertThat(viewModel.state.value.inFlight).isEmpty()
    }

    private class FakeRepository(
        private val restoreResult: Boolean = false,
        private val restoreFailure: Exception? = null,
        private val disconnectFailure: Exception? = null,
        private val patchGate: CompletableDeferred<Unit>? = null,
    ) : ApiHubDataSource {
        val adapters = listOf(
            AdapterDescriptor("new-api", "New API", Capabilities(true, true, true)),
            AdapterDescriptor("sub2api", "Sub2API", Capabilities(false, true, false)),
            AdapterDescriptor("zen-api", "ZenAPI", Capabilities(true, false, false)),
        )
        val site = Site(
            id = "11111111-1111-1111-1111-111111111111",
            name = "站点",
            baseUrl = "https://station.example",
            adapter = "new-api",
            userId = "",
            enabled = true,
            checkinEnabled = true,
            announcementEnabled = true,
            checkinCron = "15 8 * * *",
            announcementCron = "*/30 * * * *",
            timezone = "Asia/Shanghai",
            credentialConfigured = true,
            consecutiveFailures = 0,
            capabilities = Capabilities(true, true, false),
            createdAt = "2026-07-17T00:00:00Z",
            updatedAt = "2026-07-17T00:00:00Z",
        )
        private val dashboard = dashboard(totalSites = 1)
        private val refreshResults = ArrayDeque<CompletableDeferred<DashboardData>>()
        var lastPatch: SitePatch? = null
        var lastCreate: SiteWrite? = null
        var disconnectCalled = false
        var refreshCallCount = 0

        fun dashboard(totalSites: Int) = DashboardData(
            summary = Summary(SiteCounts(totalSites, totalSites), mapOf("success" to 1), 0),
            adapters = adapters,
            sites = listOf(site),
            checkins = emptyList(),
            announcements = emptyList(),
        )

        fun enqueueRefresh() = CompletableDeferred<DashboardData>().also(refreshResults::addLast)

        override suspend fun restore(): Boolean {
            restoreFailure?.let { throw it }
            return restoreResult
        }
        override suspend fun connect(baseUrl: String, token: String): DashboardData = dashboard
        override suspend fun disconnect() {
            disconnectCalled = true
            disconnectFailure?.let { throw it }
        }
        override suspend fun refresh(): DashboardData {
            refreshCallCount++
            return refreshResults.removeFirstOrNull()?.await() ?: dashboard
        }
        override suspend fun createSite(input: SiteWrite): Site {
            lastCreate = input
            return site
        }
        override suspend fun patchSite(id: String, input: SitePatch): Site {
            patchGate?.await()
            lastPatch = input
            return site.copy(enabled = input.enabled ?: site.enabled)
        }
        override suspend fun runCheckin(siteId: String): CheckinRun = error("not used")
        override suspend fun syncAnnouncements(siteId: String): AnnouncementSync = error("not used")
        override suspend fun setAnnouncementRead(id: String, read: Boolean): Announcement = error("not used")
        override fun close() = Unit
    }
}
