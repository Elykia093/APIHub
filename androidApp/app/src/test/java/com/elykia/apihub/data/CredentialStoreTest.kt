package com.elykia.apihub.data

import androidx.datastore.core.okio.OkioStorage
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.PreferencesSerializer
import com.elykia.apihub.data.security.TokenCipher
import com.google.common.truth.Truth.assertThat
import kotlinx.coroutines.test.runTest
import okio.FileSystem
import okio.Path.Companion.toPath
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

class CredentialStoreTest {
    @get:Rule
    val temporaryFolder = TemporaryFolder()

    @Test
    fun tokenRoundTripsWithoutPlaintextOnDisk() = runTest {
        val file = temporaryFolder.root.resolve("credentials.preferences_pb")
        val cipher = XorCipher()
        val dataStore = testDataStore(file)
        val expected = StoredCredentials("https://hub.example", "top-secret-token")

        CredentialStore(dataStore, cipher).save(expected)
        val restoredAfterRecreation = CredentialStore(dataStore, cipher).load()

        assertThat(restoredAfterRecreation).isEqualTo(expected)
        val disk = file.readBytes().toString(Charsets.ISO_8859_1)
        assertThat(disk).doesNotContain(expected.token)
    }

    @Test
    fun clearRemovesCredentialsAndDeletesKey() = runTest {
        val file = temporaryFolder.root.resolve("credentials.preferences_pb")
        val cipher = XorCipher()
        val dataStore = testDataStore(file)
        val store = CredentialStore(dataStore, cipher)
        store.save(StoredCredentials("https://hub.example", "token"))

        store.clear()

        assertThat(store.load()).isNull()
        assertThat(cipher.deleted).isTrue()
    }

    private fun kotlinx.coroutines.test.TestScope.testDataStore(file: java.io.File) =
        PreferenceDataStoreFactory.create(
            storage = OkioStorage(
                fileSystem = FileSystem.SYSTEM,
                serializer = PreferencesSerializer,
                producePath = { file.absolutePath.toPath() },
            ),
            scope = backgroundScope,
        )

    private class XorCipher : TokenCipher {
        var deleted = false

        override fun encrypt(plainText: ByteArray): ByteArray = plainText.map { (it.toInt() xor MASK).toByte() }.toByteArray()
        override fun decrypt(payload: ByteArray): ByteArray = encrypt(payload)
        override fun deleteKey() {
            deleted = true
        }

        companion object {
            private const val MASK = 0x5A
        }
    }
}
