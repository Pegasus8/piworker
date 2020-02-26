<template>
  <v-app-bar elevate-on-scroll dark dense app>
    <v-fade-transition>
      <v-app-bar-nav-icon
        v-show="isAuthenticated"
        @click.stop="expandNavDrawer"
      />
    </v-fade-transition>
     <v-toolbar-title class="font-weight-bold" >
        PiWorker <span class="overline">v0.1.0-alpha</span>
      </v-toolbar-title>
      <v-spacer/>
      <v-menu
        offset-y
      >
        <template v-slot:activator="{ on }">
          <v-fade-transition>
            <v-btn icon v-on="on" v-show="isAuthenticated">
              <v-icon>mdi-dots-vertical</v-icon>
            </v-btn>
          </v-fade-transition>
        </template>
        <v-list>
          <v-list-item
            :key="$uuid.v4()"
            @click="logout"
          >
            <v-list-item-title>Logout</v-list-item-title>
          </v-list-item>
        </v-list>
      </v-menu>
  </v-app-bar>
</template>

<script>
import { mapGetters, mapActions } from 'vuex'

export default {
  computed: {
    ...mapGetters('auth', [
      'isAuthenticated'
    ])
  },
  methods: {
    ...mapActions('auth', [
      'logout'
    ]),
    expandNavDrawer () {
      this.$emit('expandDrawer')
    }
  }
}
</script>

<style lang="scss" scoped>
</style>
