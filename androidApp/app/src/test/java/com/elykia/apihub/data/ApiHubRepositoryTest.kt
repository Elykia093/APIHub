package com.elykia.apihub.data

import androidx.datastore.core.okio.OkioStorage
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.PreferencesSerializer
import com.elykia.apihub.data.api.ApiClient
import com.elykia.apihub.data.api.ApiException
import com.elykia.apihub.data.security.TokenCipher
import com.google.common.truth.Truth.assertThat
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.runTest
import okhttp3.mockwebserver.Dispatcher
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.RecordedRequest
import okio.FileSystem
import okio.Path.Companion.toPath
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

class ApiHubRepositoryTest {
    @get:Rule
    val temporaryFolder = TemporaryFolder()

    @Test
    fun failedDashboardLoadKeepsStoredCredentialsAndActiveClient() = runTest {
        val oldServer = dashboardServer(siteTotal = 1)
        val candidateServer = dashboardServer(siteTotal = 2, failingPath = ANNOUNCEMENTS_PATH)
        val store = credentialStore()
        val repository = repository(store)
        try {
            val oldCredentials = StoredCredentials(oldServer.url("/").toString().trimEnd('/'), "old-token")
            repository.connect(oldCredentials.baseUrl, oldCredentials.token)

            val error = runCatching {
                repository.connect(candidateServer.url("/").toString(), "new-token")
            }.exceptionOrNull()

            assertThat(error).isInstanceOf(ApiException::class.java)
            assertThat(store.load()).isEqualTo(oldCredentials)
            assertThat(repository.refresh().summary.sites.total).isEqualTo(1)
        } finally {
            repository.close()
            oldServer.shutdown()
            candidateServer.shutdown()
        }
    }

    @Test
    fun successfulDashboardLoadCommitsCredentialsAndActiveClient() = runTest {
        val oldServer = dashboardServer(siteTotal = 1)
        val candidateServer = dashboardServer(siteTotal = 2)
        val store = credentialStore()
        val repository = repository(store)
        try {
            repository.connect(oldServer.url("/").toString(), "old-token")
            val newCredentials = StoredCredentials(candidateServer.url("/").toString().trimEnd('/'), "new-token")

            val dashboard = repository.connect(newCredentials.baseUrl, newCredentials.token)

            assertThat(dashboard.summary.sites.total).isEqualTo(2)
            assertThat(store.load()).isEqualTo(newCredentials)
            assertThat(repository.refresh().summary.sites.total).isEqualTo(2)
        } finally {
            repository.close()
            oldServer.shutdown()
            candidateServer.shutdown()
        }
    }

    private fun repository(store: CredentialStore) = ApiHubRepository(store) { baseUrl, token ->
        ApiClient(baseUrl, token, allowHttp = true)
    }

    private fun TestScope.credentialStore(): CredentialStore {
        val file = temporaryFolder.root.resolve("credentials-${System.nanoTime()}.preferences_pb")
        val dataStore = PreferenceDataStoreFactory.create(
            storage = OkioStorage(
                fileSystem = FileSystem.SYSTEM,
                serializer = PreferencesSerializer,
                producePath = { file.absolutePath.toPath() },
            ),
            scope = backgroundScope,
        )
        return CredentialStore(dataStore, XorCipher())
    }

    private fun dashboardServer(siteTotal: Int, failingPath: String? = null): MockWebServer =
        MockWebServer().apply {
            dispatcher = object : Dispatcher() {
                override fun dispatch(request: RecordedRequest): MockResponse {
                    if (request.path == failingPath) {
                        return response(
                            500,
                            """{"error":{"code":"DASHBOARD_FAILED","message":"dashboard failed","retryable":true,"requestId":"request-test"}}""",
                        )
                    }
                    return when (request.path) {
                        "/api/v1/summary" -> response(
                            200,
                            """{"sites":{"total":$siteTotal,"enabled":$siteTotal},"today":{},"unreadAnnouncements":0}""",
                        )
                        "/api/v1/site-adapters",
                        "/api/v1/sites",
                        "/api/v1/checkin-runs?limit=100",
                        ANNOUNCEMENTS_PATH,
                        -> response(200, """{"data":[]}""")
                        else -> response(404, """{"error":{"code":"NOT_FOUND","message":"not found"}}""")
                    }
                }
            }
            start()
        }

    private fun response(status: Int, body: String): MockResponse =
        MockResponse()
            .setResponseCode(status)
            .addHeader("Content-Type", "application/json")
            .setBody(body)

    private class XorCipher : TokenCipher {
        override fun encrypt(plainText: ByteArray): ByteArray =
            plainText.map { (it.toInt() xor MASK).toByte() }.toByteArray()

        override fun decrypt(payload: ByteArray): ByteArray = encrypt(payload)
        override fun deleteKey() = Unit

        companion object {
            private const val MASK = 0x5A
        }
    }

    companion object {
        private const val ANNOUNCEMENTS_PATH = "/api/v1/announcements?limit=100"
    }
}
