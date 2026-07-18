package com.elykia.apihub.data.api

import com.elykia.apihub.data.model.SitePatch
import com.elykia.apihub.data.model.SiteWrite
import com.google.common.truth.Truth.assertThat
import kotlinx.coroutines.test.runTest
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.SocketPolicy
import org.junit.After
import org.junit.Before
import org.junit.Test

class ApiClientTest {
    private lateinit var server: MockWebServer
    private lateinit var client: ApiClient

    @Before
    fun setUp() {
        server = MockWebServer()
        server.start()
        client = ApiClient(server.url("/").toString(), "admin-secret", allowHttp = true)
    }

    @After
    fun tearDown() {
        client.close()
        server.shutdown()
    }

    @Test
    fun summaryAddsBearerTokenAndDecodesResponse() = runTest {
        enqueue("""{"sites":{"total":2,"enabled":1},"today":{"success":1},"unreadAnnouncements":3}""")

        val summary = client.summary()

        assertThat(summary.sites.total).isEqualTo(2)
        assertThat(summary.today["success"]).isEqualTo(1)
        assertThat(server.takeRequest().getHeader("Authorization")).isEqualTo("Bearer admin-secret")
    }

    @Test
    fun adaptersDecodeServerCapabilities() = runTest {
        enqueue(
            """{"data":[{"name":"sub2api","displayName":"Sub2API","capabilities":{"checkin":false,"announcements":true,"requiresUserId":false}}]}""",
        )

        val adapters = client.adapters()

        assertThat(adapters).hasSize(1)
        assertThat(adapters.single().name).isEqualTo("sub2api")
        assertThat(adapters.single().capabilities.checkin).isFalse()
        assertThat(adapters.single().capabilities.announcements).isTrue()
        assertThat(server.takeRequest().path).isEqualTo("/api/v1/site-adapters")
    }

    @Test
    fun dashboardEndpointsCanBeLoadedFromOneClient() = runTest {
        enqueue("""{"sites":{"total":1,"enabled":1},"today":{},"unreadAnnouncements":0}""")
        enqueue("""{"data":[]}""")
        enqueue("""{"data":[]}""")
        enqueue("""{"data":[]}""")
        enqueue("""{"data":[]}""")

        val summary = client.summary()
        val adapters = client.adapters()
        val sites = client.sites()
        val checkins = client.checkins()
        val announcements = client.announcements()

        assertThat(summary.sites.total).isEqualTo(1)
        assertThat(adapters).isEmpty()
        assertThat(sites).isEmpty()
        assertThat(checkins).isEmpty()
        assertThat(announcements).isEmpty()
    }

    @Test
    fun patchKeepsExplicitFalseAndOmitsAbsentFields() = runTest {
        enqueue(siteEnvelope(enabled = false))

        val site = client.patchSite(SITE_ID, SitePatch(enabled = false))

        assertThat(site.enabled).isFalse()
        val body = server.takeRequest().body.readUtf8()
        assertThat(body).isEqualTo("{\"enabled\":false}")
        assertThat(body).doesNotContain("accessToken")
    }

    @Test
    fun createLetsAutomaticDetectionChooseCapabilityDefaults() = runTest {
        enqueue(siteEnvelope(enabled = true))

        client.createSite(
            SiteWrite(
                name = "自动站点",
                baseUrl = "https://station.example",
                adapter = "auto",
                userId = "",
                accessToken = "station-token",
                enabled = true,
                checkinCron = "15 8 * * *",
                announcementCron = "*/30 * * * *",
                timezone = "Asia/Shanghai",
            ),
        )

        val body = server.takeRequest().body.readUtf8()
        assertThat(body).contains("\"adapter\":\"auto\"")
        assertThat(body).doesNotContain("checkinEnabled")
        assertThat(body).doesNotContain("announcementEnabled")
    }

    @Test
    fun structuredErrorIsPreserved() = runTest {
        server.enqueue(
            MockResponse()
                .setResponseCode(401)
                .addHeader("Content-Type", "application/json")
                .setBody("""{"error":{"code":"AUTH_REQUIRED","message":"Administrator authentication required","retryable":false,"requestId":"request-1"}}"""),
        )

        val error = runCatching { client.summary() }.exceptionOrNull() as ApiException

        assertThat(error.status).isEqualTo(401)
        assertThat(error.code).isEqualTo("AUTH_REQUIRED")
        assertThat(error.requestId).isEqualTo("request-1")
    }

    @Test
    fun redirectIsNotFollowedWithBearerToken() = runTest {
        val redirected = MockWebServer()
        redirected.start()
        try {
            server.enqueue(
                MockResponse()
                    .setResponseCode(302)
                    .addHeader("Location", redirected.url("/stolen")),
            )

            val error = runCatching { client.summary() }.exceptionOrNull() as ApiException

            assertThat(error.status).isEqualTo(302)
            assertThat(redirected.requestCount).isEqualTo(0)
        } finally {
            redirected.shutdown()
        }
    }

    @Test
    fun weakNetworkTimeoutHasStableRetryableError() = runTest {
        client.close()
        client = ApiClient(
            rawBaseUrl = server.url("/").toString(),
            token = "admin-secret",
            allowHttp = true,
            requestTimeoutMillis = 100,
            connectTimeoutMillis = 100,
        )
        server.enqueue(MockResponse().setSocketPolicy(SocketPolicy.NO_RESPONSE))

        val error = runCatching { client.summary() }.exceptionOrNull() as ApiException

        assertThat(error.code).isEqualTo("UPSTREAM_TIMEOUT")
        assertThat(error.retryable).isTrue()
    }

    @Test
    fun debugHttpIsLimitedToLocalDevelopmentHosts() {
        for (url in listOf(
            "http://localhost:4180",
            "http://127.0.0.1:4180",
            "http://10.0.2.2:4180",
            "http://192.168.1.20:4180",
            "http://apihub.local:4180",
        )) {
            assertThat(ApiClient.normalizeBaseUrl(url, allowHttp = true)).isEqualTo(url)
        }

        for (url in listOf("http://example.com", "http://fc-example.com")) {
            val error = runCatching {
                ApiClient.normalizeBaseUrl(url, allowHttp = true)
            }.exceptionOrNull()
            assertThat(error).isInstanceOf(IllegalArgumentException::class.java)
        }
    }

    @Test
    fun releaseRejectsHttpEvenForLocalHosts() {
        val error = runCatching {
            ApiClient.normalizeBaseUrl("http://127.0.0.1:4180", allowHttp = false)
        }.exceptionOrNull()
        assertThat(error).isInstanceOf(IllegalArgumentException::class.java)
    }

    private fun enqueue(body: String) {
        server.enqueue(MockResponse().setResponseCode(200).addHeader("Content-Type", "application/json").setBody(body))
    }

    private fun siteEnvelope(enabled: Boolean): String = """
        {"data":{"id":"$SITE_ID","name":"站点","baseUrl":"https://station.example","adapter":"new-api","userId":"","enabled":$enabled,"checkinEnabled":true,"announcementEnabled":true,"checkinCron":"15 8 * * *","announcementCron":"*/30 * * * *","timezone":"Asia/Shanghai","credentialConfigured":true,"consecutiveFailures":0,"capabilities":{"checkin":true,"announcements":true,"requiresUserId":false},"createdAt":"2026-07-17T00:00:00Z","updatedAt":"2026-07-17T00:00:00Z"}}
    """.trimIndent()

    companion object {
        private const val SITE_ID = "11111111-1111-1111-1111-111111111111"
    }
}
