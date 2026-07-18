package com.elykia.apihub.data.security

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

interface TokenCipher {
    fun encrypt(plainText: ByteArray): ByteArray
    fun decrypt(payload: ByteArray): ByteArray
    fun deleteKey()
}

class AndroidKeystoreCipher(
    private val alias: String = "apihub-admin-token-v1",
) : TokenCipher {
    private val keyStore: KeyStore
        get() = KeyStore.getInstance(ANDROID_KEYSTORE).apply { load(null) }

    override fun encrypt(plainText: ByteArray): ByteArray {
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.ENCRYPT_MODE, key())
        val encrypted = cipher.doFinal(plainText)
        return byteArrayOf(FORMAT_VERSION, cipher.iv.size.toByte()) + cipher.iv + encrypted
    }

    override fun decrypt(payload: ByteArray): ByteArray {
        require(payload.size > HEADER_SIZE && payload[0] == FORMAT_VERSION) { "Unsupported token ciphertext" }
        val ivSize = payload[1].toInt() and 0xff
        require(ivSize in 12..16 && payload.size > HEADER_SIZE + ivSize) { "Invalid token ciphertext" }
        val iv = payload.copyOfRange(HEADER_SIZE, HEADER_SIZE + ivSize)
        val encrypted = payload.copyOfRange(HEADER_SIZE + ivSize, payload.size)
        val cipher = Cipher.getInstance(TRANSFORMATION)
        cipher.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(TAG_BITS, iv))
        return cipher.doFinal(encrypted)
    }

    override fun deleteKey() {
        keyStore.deleteEntry(alias)
    }

    private fun key(): SecretKey {
        val existing = keyStore.getKey(alias, null) as? SecretKey
        if (existing != null) return existing
        return KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, ANDROID_KEYSTORE).run {
            init(
                KeyGenParameterSpec.Builder(
                    alias,
                    KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
                )
                    .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                    .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                    .setKeySize(256)
                    .setRandomizedEncryptionRequired(true)
                    .build(),
            )
            generateKey()
        }
    }

    companion object {
        private const val ANDROID_KEYSTORE = "AndroidKeyStore"
        private const val TRANSFORMATION = "AES/GCM/NoPadding"
        private const val TAG_BITS = 128
        private const val HEADER_SIZE = 2
        private const val FORMAT_VERSION: Byte = 1
    }
}
