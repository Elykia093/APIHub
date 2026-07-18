package com.elykia.apihub.data.security

import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertFalse
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class AndroidKeystoreCipherTest {
    @Test
    fun roundTripUsesRandomizedCiphertext() {
        val cipher = AndroidKeystoreCipher("apihub-test-${System.nanoTime()}")
        val plain = "device-secret".encodeToByteArray()
        try {
            val first = cipher.encrypt(plain)
            val second = cipher.encrypt(plain)

            assertFalse(first.contentEquals(second))
            assertArrayEquals(plain, cipher.decrypt(first))
            assertArrayEquals(plain, cipher.decrypt(second))
        } finally {
            cipher.deleteKey()
        }
    }
}
