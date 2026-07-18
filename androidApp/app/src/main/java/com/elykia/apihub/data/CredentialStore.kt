package com.elykia.apihub.data

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import com.elykia.apihub.data.security.AndroidKeystoreCipher
import com.elykia.apihub.data.security.TokenCipher
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.first
import java.util.Base64

private val Context.credentialsDataStore by preferencesDataStore(name = "credentials")

data class StoredCredentials(val baseUrl: String, val token: String) {
    override fun toString(): String = "StoredCredentials(baseUrl=$baseUrl, token=<redacted>)"
}

class CredentialStore(
    private val dataStore: DataStore<Preferences>,
    private val cipher: TokenCipher,
) {
    constructor(context: Context) : this(
        dataStore = context.credentialsDataStore,
        cipher = AndroidKeystoreCipher(),
    )

    suspend fun save(credentials: StoredCredentials) {
        val encrypted = cipher.encrypt(credentials.token.encodeToByteArray())
        val encoded = Base64.getEncoder().withoutPadding().encodeToString(encrypted)
        dataStore.edit { preferences ->
            preferences[BASE_URL] = credentials.baseUrl
            preferences[ENCRYPTED_TOKEN] = encoded
        }
    }

    suspend fun load(): StoredCredentials? {
        val preferences = dataStore.data.first()
        val baseUrl = preferences[BASE_URL] ?: return null
        val encoded = preferences[ENCRYPTED_TOKEN] ?: return null
        return try {
            val decrypted = cipher.decrypt(Base64.getDecoder().decode(encoded))
            StoredCredentials(baseUrl, decrypted.decodeToString())
        } catch (error: CancellationException) {
            throw error
        } catch (_: Exception) {
            clear()
            null
        }
    }

    suspend fun clear() {
        dataStore.edit { preferences ->
            preferences.remove(BASE_URL)
            preferences.remove(ENCRYPTED_TOKEN)
        }
        runCatching { cipher.deleteKey() }
    }

    companion object {
        private val BASE_URL = stringPreferencesKey("base_url")
        private val ENCRYPTED_TOKEN = stringPreferencesKey("encrypted_admin_token")
    }
}
