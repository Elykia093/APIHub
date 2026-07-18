package com.elykia.apihub

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.lifecycle.ViewModelProvider
import com.elykia.apihub.data.ApiHubRepository
import com.elykia.apihub.data.CredentialStore
import com.elykia.apihub.ui.ApiHubApp
import com.elykia.apihub.ui.MainViewModel

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        val repository = ApiHubRepository(CredentialStore(applicationContext))
        val viewModel = ViewModelProvider(this, MainViewModel.Factory(repository))[MainViewModel::class.java]
        setContent { ApiHubApp(viewModel) }
    }
}
