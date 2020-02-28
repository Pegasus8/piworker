<template>
  <v-card outlined>

    <v-card-title>
      {{ cardTitle }}
    </v-card-title>

    <v-card-subtitle v-if="cardSubtitle !== ''" align='left'>
      {{ cardSubtitle }}
    </v-card-subtitle>

    <v-card-text>

      <draggable v-model='userElementsComputed' :disabled='dragAndDrop'>
        <div
          v-for="(userElement, index) in userElementsComputed"
          :key="userElement.internalID"
          class="my-2"
        >
          <v-row v-if='userElement.order !== 0 && cardTitle === "Actions"' class="my-1" justify='center'>
            <v-icon color='blue darken-2' large>mdi-arrow-down-bold</v-icon>
            <v-icon v-if="userElement.chained" color='blue darken-2'>mdi-link-variant</v-icon>
          </v-row>
          <v-card :elevation='4'>
            <v-card-title>
              {{ userElement.name }}
            </v-card-title>
            <v-card-subtitle>
              {{ userElement.description }}
            </v-card-subtitle>
            <v-card-text>
              <v-expansion-panels v-model="userElement.openArg">
                <v-expansion-panel v-for="arg in userElement.args" :key="arg.ID">
                  <v-expansion-panel-header
                    :disable-icon-rotate='userElement.argumentToReplaceByCR === arg.ID'
                  >
                    <template v-slot='{ open }'>
                      {{ arg.name }}
                      <v-fade-transition leave-absolute>
                        <span
                          v-if="
                            !open &&
                            cardTitle === 'Actions' &&
                            userElement.order !== 0 &&
                            userElementsComputed[index - 1].returnedChainResultType === arg.contentType
                          "
                          style='font-size: 8px;'
                          class="mx-2"
                        >&#x25CF;</span>
                      </v-fade-transition>
                    </template>

                    <template
                      v-if='userElement.argumentToReplaceByCR === arg.ID'
                      v-slot:actions
                    >
                      <v-icon color="blue darken-2">mdi-link-box-variant</v-icon>
                    </template>
                  </v-expansion-panel-header>
                  <v-expansion-panel-content
                    class="text--secondary"
                  >
                    {{ arg.description }}
                    <app-adaptative-arg
                      :content='arg.content'
                      :argType="arg.contentType"
                      :disabled='userElement.argumentToReplaceByCR === arg.ID && userElement.chained'
                      @changed='arg.content = $event'
                    />
                    <div
                      v-if='userElement.order !== 0 && cardTitle === "Actions"'
                      class="d-flex flex-row-reverse"
                    >
                      <div v-if='userElementsComputed[index - 1].returnedChainResultType === arg.contentType'>
                        <v-btn
                          v-if="userElement.argumentToReplaceByCR !== arg.ID"
                          color='blue lighten-1'
                          @click='setArgChained(userElement, arg.ID)'
                          icon
                        >
                          <v-icon>mdi-link-variant</v-icon>
                        </v-btn>
                        <v-btn
                          v-else
                          color='red lighten-1'
                          @click='removeArgChained(userElement)'
                          icon
                        >
                          <v-icon>mdi-link-variant-off</v-icon>
                        </v-btn>
                      </div>
                      <div v-else>
                        <v-tooltip left>
                          <template v-slot:activator="{ on }">
                            <v-icon v-on="on" color='red lighten-1'>mdi-shield-link-variant</v-icon>
                          </template>
                          <span>This argument is not compatible with the previous result.</span>
                        </v-tooltip>
                      </div>
                    </div>
                  </v-expansion-panel-content>
                </v-expansion-panel>
              </v-expansion-panels>
            </v-card-text>
            <v-card-actions>
              <div v-if="cardTitle === 'Actions'" class="text--secondary caption font-weight-light">
                Returned type: {{ userElement.returnedChainResultType }}
              </div>
              <v-spacer/>
              <v-btn
                color='red lighten-1'
                @click="removeItem(userElement.internalID)"
                text
                icon
              >
                <v-icon>mdi-delete</v-icon>
              </v-btn>
            </v-card-actions>
          </v-card>
        </div>
      </draggable>

      <v-row justify='center'>
        <v-btn color='green' @click="openSelector" text icon>
          <v-icon>mdi-plus</v-icon>
        </v-btn>
      </v-row>
    </v-card-text>
  </v-card>
</template>

<script>
import draggable from 'vuedraggable'
import AdaptativeArgSelector from './AdaptativeArgSelector.vue'

export default {
  props: {
    cardTitle: {
      type: String,
      required: true
    },
    cardSubtitle: {
      type: String,
      required: false,
      default: ''
    },
    userElements: {
      type: Array,
      required: true
    },
    dragAndDrop: {
      type: Boolean,
      required: false,
      default: true
    }
  },
  data () {
    return {
    }
  },
  computed: {
    userElementsComputed: {
      get () {
        return this.userElements
      },
      set (newValue) {
        this.$emit('modified', newValue)
      }
    }
  },
  methods: {
    openSelector () {
      this.$emit('open-selector')
    },
    removeItem (elementInternalID) {
      this.$emit('remove-item', elementInternalID)
    },
    orderUpdateRequired () {
      this.$emit('order-modified')
    },
    setArgChained (action, argID) {
      action.argumentToReplaceByCR = argID
      action.chained = true
      // Change the key of the component to force an update of the UI. Otherwise,
      // the UI won't be updated until the next event.
      action.internalID = this.$uuid.v4()
    },
    removeArgChained (action) {
      action.argumentToReplaceByCR = ''
      action.chained = false
      // Change the key of the component to force an update of the UI. Otherwise,
      // the UI won't be updated until the next event.
      action.internalID = this.$uuid.v4()
    }
  },
  watch: {
    userElementsComputed: function (newValue) {
      this.orderUpdateRequired()
    }
  },
  components: {
    draggable,
    appAdaptativeArg: AdaptativeArgSelector
  }
}
</script>
